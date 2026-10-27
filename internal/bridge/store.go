package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Run struct {
	Token       string    `json:"token"`
	ChatID      int64     `json:"chat_id"`
	UserID      int64     `json:"user_id"`
	ConnectorID string    `json:"connector_id"`
	ComposerID  string    `json:"background_composer_id"`
	RunUUID     string    `json:"run_uuid"`
	Started     time.Time `json:"started"`
	TimeoutAt   time.Time `json:"timeout_at"`
	Expires     time.Time `json:"expires"`
	Reminded    bool      `json:"reminded"`
	Reported    bool      `json:"reported"`
}

type Store struct {
	mu   sync.Mutex
	path string
	runs map[string]Run
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	store := &Store{path: path, runs: map[string]Run{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(raw, &store.runs); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Save(run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.Token] = run
	return s.flush()
}

func (s *Store) ByToken(token string) (Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[token]
	return run, ok
}

func (s *Store) Active(connectorID string, now time.Time) (Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found Run
	ok := false
	for _, run := range s.runs {
		if run.ConnectorID != connectorID || run.Reported || !now.Before(run.TimeoutAt) {
			continue
		}
		if !ok || run.Started.After(found.Started) {
			found = run
			ok = true
		}
	}
	return found, ok
}

func (s *Store) Update(token string, fn func(*Run)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[token]
	if !ok {
		return os.ErrNotExist
	}
	fn(&run)
	s.runs[token] = run
	return s.flush()
}

func (s *Store) DueReminder(now time.Time) []Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []Run
	for _, run := range s.runs {
		if !run.Reported && !run.Reminded && !now.Before(run.TimeoutAt) && now.Before(run.Expires) {
			due = append(due, run)
		}
	}
	return due
}

func (s *Store) Purge(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for token, run := range s.runs {
		if !now.Before(run.Expires) {
			delete(s.runs, token)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.flush()
}

func (s *Store) flush() error {
	raw, err := json.MarshalIndent(s.runs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
