package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"external-display-position/internal/display"
)

// Profile stores the desired positions for a specific monitor+port topology signature.
type Profile struct {
	Name      string                      `json:"name,omitempty"`
	Positions map[string]display.Position `json:"positions"`
	Displays  []display.DisplayInfo       `json:"displays,omitempty"`
	UpdatedAt time.Time                   `json:"updatedAt"`
}

// Config is the root configuration persisted locally on the machine.
type Config struct {
	Profiles map[string]Profile `json:"profiles"`
}

// DefaultPath returns %LOCALAPPDATA%\ExternalDisplayPosition\config.json.
func DefaultPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if exe, err := os.Executable(); err == nil {
			base = filepath.Dir(exe)
		} else {
			base = "."
		}
		return filepath.Join(base, "config.json")
	}
	return filepath.Join(base, "ExternalDisplayPosition", "config.json")
}

// DefaultLogPath returns %LOCALAPPDATA%\ExternalDisplayPosition\edp.log.
func DefaultLogPath() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "edp.log")
}

// Load reads config from path. If the file does not exist, an empty Config is returned.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Profiles: make(map[string]Profile)}, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	return &cfg, nil
}

// Save writes cfg atomically to path.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %q: %w", dir, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	return nil
}

// UpsertSnapshot saves or updates the profile for snap in cfg.
func (c *Config) UpsertSnapshot(name string, snap *display.Snapshot) Profile {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	if name == "" {
		if existing, ok := c.Profiles[snap.Signature]; ok {
			name = existing.Name
		}
	}

	positions := make(map[string]display.Position, len(snap.Displays))
	for _, d := range snap.Displays {
		positions[d.ID] = display.Position{X: d.X, Y: d.Y}
	}

	prof := Profile{
		Name:      name,
		Positions: positions,
		Displays:  snap.Displays,
		UpdatedAt: time.Now(),
	}
	c.Profiles[snap.Signature] = prof
	return prof
}

// Delete removes a profile matching either signature or profile name (case-insensitive).
func (c *Config) Delete(key string) (string, bool) {
	if _, ok := c.Profiles[key]; ok {
		delete(c.Profiles, key)
		return key, true
	}
	for sig, prof := range c.Profiles {
		if prof.Name != "" && strings.EqualFold(prof.Name, key) {
			delete(c.Profiles, sig)
			return sig, true
		}
	}
	return "", false
}
