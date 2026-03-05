// Package auth provides 2-token authentication management for the Rediver agent.
package auth

import (
	"os"
	"path/filepath"
	"strings"
)

// Persister handles agent ID file read/write with atomic operations.
type Persister struct {
	path string
}

// NewPersister creates a Persister with the given file path.
func NewPersister(path string) *Persister {
	return &Persister{path: path}
}

// DefaultAgentIDPath returns the default agent ID file path (~/.rediver/agent_id).
func DefaultAgentIDPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".rediver", "agent_id")
}

// ReadAgentID reads the cached agent ID from file.
// Returns empty string (not error) if file doesn't exist.
func (p *Persister) ReadAgentID() (string, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteAgentID writes the agent ID atomically (write temp + rename).
func (p *Persister) WriteAgentID(id string) error {
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "agent_id_*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(id); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, p.path)
}
