package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store loads, mutates, and atomically persists the state model.
type Store struct {
	path  string
	mu    sync.Mutex
	m     Model
	dirty bool // in-memory state changed but the last durable write failed
}

// Load reads path. A missing file yields empty state. A corrupt file is
// renamed aside and empty state is returned so the daemon can still start.
func Load(path string) (*Store, error) {
	s := &Store{path: path, m: Model{Version: version, TCPPorts: map[string]int{}}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		backup := fmt.Sprintf("%s.corrupt.%d", path, time.Now().Unix())
		// #nosec G703 -- backup remains adjacent to the configured state file with a fixed suffix.
		if backupErr := os.WriteFile(backup, data, 0o600); backupErr != nil {
			return nil, fmt.Errorf("state file %s is corrupt and could not be backed up: %w", path, backupErr)
		}
		return s, nil
	}
	if m.Version > version {
		return nil, fmt.Errorf("state file %s has unsupported version %d (want <= %d); refusing to overwrite", path, m.Version, version)
	}
	if m.TCPPorts == nil {
		m.TCPPorts = map[string]int{}
	}
	if m.Version == 0 {
		m.Version = version
	}
	s.m = m
	return s, nil
}

// Port returns the remembered frontend port for name, or 0.
func (s *Store) Port(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.TCPPorts[name]
}

// SetPort remembers name -> port and persists immediately. If a previous
// write failed, the dirty flag forces a retry even when the requested value is
// already in memory; returning nil must mean the state is durable.
func (s *Store) SetPort(name string, port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m.TCPPorts[name] == port && !s.dirty {
		return nil
	}
	s.m.TCPPorts[name] = port
	s.dirty = true
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Forget removes a remembered assignment.
func (s *Store) Forget(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m.TCPPorts[name]; !ok && !s.dirty {
		return nil
	}
	delete(s.m.TCPPorts, name)
	s.dirty = true
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Snapshot returns a copy of the persisted map.
func (s *Store) Snapshot() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.m.TCPPorts))
	for k, v := range s.m.TCPPorts {
		out[k] = v
	}
	return out
}

// Save flushes current state.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}
