package contacts

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type Store struct {
	db         *sql.DB
	instanceID string
}

type Contact struct {
	RemoteJID     string
	PushName      string
	ProfilePicURL string
}

type MessageRecord struct {
	RemoteJID   string
	PushName    string
	MessageType string
	TextPreview string
	Timestamp   time.Time
	FromMe      bool
}

func NewStore(databaseURL, instanceID string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &Store{db: db, instanceID: instanceID}, nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SearchContacts(query string) ([]Contact, error) {
	q := `
		SELECT "remoteJid", COALESCE("pushName", ''), COALESCE("profilePicUrl", '')
		FROM "Contact"
		WHERE "instanceId" = $1
		  AND "pushName" IS NOT NULL
		  AND "pushName" != ''
		  AND "pushName" ILIKE $2
		ORDER BY "updatedAt" DESC
		LIMIT 20`

	rows, err := s.db.Query(q, s.instanceID, "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("searching contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.RemoteJID, &c.PushName, &c.ProfilePicURL); err != nil {
			return nil, fmt.Errorf("scanning contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *Store) ResolveJID(nameOrJID string) (string, string, error) {
	if strings.Contains(nameOrJID, "@") {
		name, err := s.jidToName(nameOrJID)
		return nameOrJID, name, err
	}

	q := `
		SELECT "remoteJid", COALESCE("pushName", '')
		FROM "Contact"
		WHERE "instanceId" = $1
		  AND "pushName" ILIKE $2
		ORDER BY "updatedAt" DESC
		LIMIT 1`

	var jid, name string
	err := s.db.QueryRow(q, s.instanceID, "%"+nameOrJID+"%").Scan(&jid, &name)
	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("no contact matching %q", nameOrJID)
	}
	if err != nil {
		return "", "", fmt.Errorf("resolving contact: %w", err)
	}
	return jid, name, nil
}

func (s *Store) jidToName(jid string) (string, error) {
	q := `
		SELECT COALESCE("pushName", '')
		FROM "Contact"
		WHERE "instanceId" = $1 AND "remoteJid" = $2
		LIMIT 1`

	var name string
	err := s.db.QueryRow(q, s.instanceID, jid).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

func (s *Store) ResolveName(jid string) string {
	name, _ := s.jidToName(jid)
	if name == "" {
		return jid
	}
	return name
}

func (s *Store) MessageHistory(jid string, limit int) ([]MessageRecord, error) {
	q := `
		SELECT
			key->>'remoteJid',
			COALESCE("pushName", ''),
			"messageType",
			COALESCE(
				message->>'conversation',
				message->>'text',
				''
			),
			to_timestamp("messageTimestamp") AT TIME ZONE 'UTC',
			COALESCE((key->>'fromMe')::boolean, false)
		FROM "Message"
		WHERE "instanceId" = $1
		  AND key->>'remoteJid' = $2
		ORDER BY "messageTimestamp" DESC
		LIMIT $3`

	rows, err := s.db.Query(q, s.instanceID, jid, limit)
	if err != nil {
		return nil, fmt.Errorf("querying message history: %w", err)
	}
	defer rows.Close()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.RemoteJID, &m.PushName, &m.MessageType, &m.TextPreview, &m.Timestamp, &m.FromMe); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) RecentConversations(limit int) ([]MessageRecord, error) {
	q := `
		SELECT jid, name, msg_type, preview, ts, from_me FROM (
			SELECT DISTINCT ON (key->>'remoteJid')
				key->>'remoteJid' AS jid,
				COALESCE("pushName", '') AS name,
				"messageType" AS msg_type,
				COALESCE(
					message->>'conversation',
					message->>'text',
					''
				) AS preview,
				to_timestamp("messageTimestamp") AT TIME ZONE 'UTC' AS ts,
				COALESCE((key->>'fromMe')::boolean, false) AS from_me
			FROM "Message"
			WHERE "instanceId" = $1
			  AND COALESCE((key->>'fromMe')::boolean, false) = false
			ORDER BY key->>'remoteJid', "messageTimestamp" DESC
		) sub
		ORDER BY ts DESC
		LIMIT $2`

	rows, err := s.db.Query(q, s.instanceID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying recent conversations: %w", err)
	}
	defer rows.Close()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.RemoteJID, &m.PushName, &m.MessageType, &m.TextPreview, &m.Timestamp, &m.FromMe); err != nil {
			return nil, fmt.Errorf("scanning conversation: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) TodayMessages() ([]MessageRecord, error) {
	q := `
		SELECT
			key->>'remoteJid',
			COALESCE("pushName", ''),
			"messageType",
			COALESCE(
				message->>'conversation',
				message->>'text',
				''
			),
			to_timestamp("messageTimestamp") AT TIME ZONE 'UTC',
			COALESCE((key->>'fromMe')::boolean, false)
		FROM "Message"
		WHERE "instanceId" = $1
		  AND "messageTimestamp" >= extract(epoch from (now() - interval '24 hours'))::integer
		  AND COALESCE((key->>'fromMe')::boolean, false) = false
		ORDER BY "messageTimestamp" ASC`

	rows, err := s.db.Query(q, s.instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying today messages: %w", err)
	}
	defer rows.Close()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.RemoteJID, &m.PushName, &m.MessageType, &m.TextPreview, &m.Timestamp, &m.FromMe); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

type ContactWithHistory struct {
	JID      string
	PushName string
	History  string
}

func (s *Store) UncategorisedWithHistory(msgLimit, minMsgs int) ([]ContactWithHistory, error) {
	q := `
		WITH ranked AS (
			SELECT
				key->>'remoteJid'                                        AS jid,
				COALESCE("pushName", '')                                 AS push_name,
				"messageType",
				COALESCE(message->>'conversation', message->>'text', '') AS text,
				COALESCE((key->>'fromMe')::boolean, false)               AS from_me,
				"messageTimestamp",
				ROW_NUMBER() OVER (
					PARTITION BY key->>'remoteJid'
					ORDER BY "messageTimestamp" DESC
				) AS rn,
				COUNT(*) OVER (PARTITION BY key->>'remoteJid')           AS msg_count
			FROM "Message"
			WHERE "instanceId" = $1
			  AND key->>'remoteJid' NOT LIKE '%@g.us'
		)
		SELECT r.jid, r.push_name, r."messageType", r.text, r.from_me
		FROM ranked r
		LEFT JOIN ww_contacts wc ON wc.jid = r.jid
		WHERE r.rn <= $2
		  AND r.msg_count >= $3
		  AND (wc.jid IS NULL OR wc.category = 'unknown')
		ORDER BY r.jid, r."messageTimestamp" ASC`

	rows, err := s.db.Query(q, s.instanceID, msgLimit, minMsgs)
	if err != nil {
		return nil, fmt.Errorf("uncategorised with history: %w", err)
	}
	defer rows.Close()

	byJID := make(map[string]*ContactWithHistory)
	var order []string
	for rows.Next() {
		var jid, pushName, msgType, text string
		var fromMe bool
		if err := rows.Scan(&jid, &pushName, &msgType, &text, &fromMe); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		if _, ok := byJID[jid]; !ok {
			byJID[jid] = &ContactWithHistory{JID: jid, PushName: pushName}
			order = append(order, jid)
		}
		dir := "[them]"
		if fromMe {
			dir = "[you]"
		}
		content := text
		if content == "" {
			content = fmt.Sprintf("[%s]", msgType)
		}
		byJID[jid].History += fmt.Sprintf("%s %s\n", dir, content)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating history: %w", err)
	}

	results := make([]ContactWithHistory, 0, len(order))
	for _, jid := range order {
		results = append(results, *byJID[jid])
	}
	return results, nil
}

func (s *Store) ContactStats() (int, int, error) {
	var contacts, messages int
	err := s.db.QueryRow(`SELECT count(*) FROM "Contact" WHERE "instanceId" = $1`, s.instanceID).Scan(&contacts)
	if err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRow(`SELECT count(*) FROM "Message" WHERE "instanceId" = $1`, s.instanceID).Scan(&messages)
	if err != nil {
		return 0, 0, err
	}
	return contacts, messages, nil
}
