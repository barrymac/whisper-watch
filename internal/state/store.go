package state

import (
	"database/sql"
	"fmt"
	"log/slog"

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
