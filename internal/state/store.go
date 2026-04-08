package state

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq"
)

const migrate = `
CREATE TABLE IF NOT EXISTS ww_settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ww_muted_jids (
	jid TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS ww_contacts (
	jid             TEXT PRIMARY KEY,
	category        TEXT NOT NULL DEFAULT 'unknown',
	category_reason TEXT NOT NULL DEFAULT '',
	category_source TEXT NOT NULL DEFAULT 'unknown',
	updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ww_label_map (
	category TEXT PRIMARY KEY,
	label_id TEXT NOT NULL
);
`

type Settings struct {
	MuteGroups     bool
	TranslateAudio bool
	TranslateText  bool
	ReplyDrafts    bool
	OllamaModel    string
	MutedJIDs      map[string]bool
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(migrate); err != nil {
		return nil, fmt.Errorf("migrating state tables: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Load(defaults Settings) (Settings, error) {
	out := defaults

	rows, err := s.db.Query(`SELECT key, value FROM ww_settings`)
	if err != nil {
		return out, fmt.Errorf("loading settings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, fmt.Errorf("scanning setting: %w", err)
		}
		switch k {
		case "mute_groups":
			out.MuteGroups = v == "true"
		case "translate_audio":
			out.TranslateAudio = v == "true"
		case "translate_text":
			out.TranslateText = v == "true"
		case "reply_drafts":
			out.ReplyDrafts = v == "true"
		case "ollama_model":
			if v != "" {
				out.OllamaModel = v
			}
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	muted, err := s.loadMutedJIDs()
	if err != nil {
		return out, err
	}
	for jid := range muted {
		out.MutedJIDs[jid] = true
	}

	slog.Info("state loaded from db",
		"muteGroups", out.MuteGroups,
		"translateAudio", out.TranslateAudio,
		"translateText", out.TranslateText,
		"replyDrafts", out.ReplyDrafts,
		"ollamaModel", out.OllamaModel,
		"mutedJIDs", len(out.MutedJIDs),
	)
	return out, nil
}

func (s *Store) SetBool(key string, value bool) error {
	v := "false"
	if value {
		v = "true"
	}
	return s.setSetting(key, v)
}

func (s *Store) SetString(key, value string) error {
	return s.setSetting(key, value)
}

func (s *Store) MuteJID(jid string) error {
	_, err := s.db.Exec(
		`INSERT INTO ww_muted_jids (jid) VALUES ($1) ON CONFLICT DO NOTHING`,
		jid,
	)
	if err != nil {
		return fmt.Errorf("muting jid: %w", err)
	}
	return nil
}

func (s *Store) UnmuteJID(jid string) error {
	_, err := s.db.Exec(`DELETE FROM ww_muted_jids WHERE jid = $1`, jid)
	if err != nil {
		return fmt.Errorf("unmuting jid: %w", err)
	}
	return nil
}

func (s *Store) setSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO ww_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("saving setting %s: %w", key, err)
	}
	return nil
}

func (s *Store) loadMutedJIDs() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT jid FROM ww_muted_jids`)
	if err != nil {
		return nil, fmt.Errorf("loading muted jids: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var jid string
		if err := rows.Scan(&jid); err != nil {
			return nil, fmt.Errorf("scanning jid: %w", err)
		}
		result[jid] = true
	}
	return result, rows.Err()
}

type ContactCategory struct {
	JID            string
	Category       string
	CategoryReason string
	CategorySource string
	UpdatedAt      time.Time
}

var ValidCategories = map[string]bool{
	"personal":   true,
	"family":     true,
	"business":   true,
	"service":    true,
	"commerce":   true,
	"government": true,
	"spam":       true,
	"unknown":    true,
}

func (s *Store) SetCategory(jid, category, reason, source string) error {
	_, err := s.db.Exec(
		`INSERT INTO ww_contacts (jid, category, category_reason, category_source, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (jid) DO UPDATE
		 SET category = EXCLUDED.category,
		     category_reason = EXCLUDED.category_reason,
		     category_source = EXCLUDED.category_source,
		     updated_at = now()`,
		jid, category, reason, source,
	)
	if err != nil {
		return fmt.Errorf("setting category for %s: %w", jid, err)
	}
	return nil
}

func (s *Store) GetCategory(jid string) (*ContactCategory, error) {
	var c ContactCategory
	err := s.db.QueryRow(
		`SELECT jid, category, category_reason, category_source, updated_at
		 FROM ww_contacts WHERE jid = $1`, jid,
	).Scan(&c.JID, &c.Category, &c.CategoryReason, &c.CategorySource, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting category for %s: %w", jid, err)
	}
	return &c, nil
}

func (s *Store) ListByCategory(category string) ([]ContactCategory, error) {
	rows, err := s.db.Query(
		`SELECT jid, category, category_reason, category_source, updated_at
		 FROM ww_contacts WHERE category = $1 ORDER BY updated_at DESC`, category,
	)
	if err != nil {
		return nil, fmt.Errorf("listing category %s: %w", category, err)
	}
	defer rows.Close()

	var results []ContactCategory
	for rows.Next() {
		var c ContactCategory
		if err := rows.Scan(&c.JID, &c.Category, &c.CategoryReason, &c.CategorySource, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning contact category: %w", err)
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

func (s *Store) ListUncategorised() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT c."remoteJid"
		 FROM "Contact" c
		 LEFT JOIN ww_contacts wc ON c."remoteJid" = wc.jid
		 WHERE c."pushName" IS NOT NULL AND c."pushName" != ''
		   AND (wc.jid IS NULL OR wc.category = 'unknown')
		 AND c."remoteJid" NOT LIKE '%@g.us'
		 ORDER BY c."updatedAt" DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing uncategorised: %w", err)
	}
	defer rows.Close()

	var jids []string
	for rows.Next() {
		var jid string
		if err := rows.Scan(&jid); err != nil {
			return nil, fmt.Errorf("scanning uncategorised jid: %w", err)
		}
		jids = append(jids, jid)
	}
	return jids, rows.Err()
}

func (s *Store) SetLabelMap(category, labelID string) error {
	_, err := s.db.Exec(
		`INSERT INTO ww_label_map (category, label_id) VALUES ($1, $2)
		 ON CONFLICT (category) DO UPDATE SET label_id = EXCLUDED.label_id`,
		category, labelID,
	)
	return err
}

func (s *Store) GetLabelID(category string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT label_id FROM ww_label_map WHERE category = $1`, category).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *Store) AllLabelMaps() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT category, label_id FROM ww_label_map`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var cat, id string
		if err := rows.Scan(&cat, &id); err != nil {
			return nil, err
		}
		m[cat] = id
	}
	return m, rows.Err()
}

type CategorisedContact struct {
	JID      string
	Category string
}

func (s *Store) ListCategorised() ([]CategorisedContact, error) {
	rows, err := s.db.Query(`SELECT jid, category FROM ww_contacts WHERE category != 'unknown' ORDER BY category, jid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CategorisedContact
	for rows.Next() {
		var c CategorisedContact
		if err := rows.Scan(&c.JID, &c.Category); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CategoryStats() (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT category, count(*) FROM ww_contacts GROUP BY category ORDER BY count(*) DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("category stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var cat string
		var count int
		if err := rows.Scan(&cat, &count); err != nil {
			return nil, fmt.Errorf("scanning category stat: %w", err)
		}
		stats[cat] = count
	}
	return stats, rows.Err()
}
