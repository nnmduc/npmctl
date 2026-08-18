// Package config loads npmctl's profile configuration.
//
// Profiles exist so one binary can address several NPM instances — typically a
// disposable lab and a production box — without ever mixing their credentials.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"gopkg.in/yaml.v3"
)

// Profile is one NPM instance.
type Profile struct {
	URL      string `yaml:"url"`
	Identity string `yaml:"identity,omitempty"`

	// InsecureSkipVerify disables TLS verification for this profile. Kept
	// per-profile rather than global so `-p lab --insecure` cannot silently
	// weaken a later prod call in the same shell.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty"`

	// CACert and PinSHA256 solve the self-signed homelab case WITHOUT disabling
	// verification, which is why they exist alongside InsecureSkipVerify.
	CACert    string `yaml:"ca_cert,omitempty"`
	PinSHA256 string `yaml:"pin_sha256,omitempty"`
}

// Config is the whole configuration file.
type Config struct {
	DefaultProfile string              `yaml:"default_profile"`
	Profiles       map[string]*Profile `yaml:"profiles"`

	path string `yaml:"-"`
}

// DefaultPath returns ~/.config/npmctl/config.yaml.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "npmctl", "config.yaml"), nil
}

// Load reads the configuration, returning an empty one when the file is absent —
// a fresh install has no config and `auth login --url` is expected to create it.
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	cfg := &Config{Profiles: map[string]*Profile{}, path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, exitcode.Wrap(exitcode.Usage, err, "parse %s", path)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	cfg.path = path
	return cfg, nil
}

// Path returns the file this config was loaded from.
func (c *Config) Path() string { return c.path }

// Save writes the configuration with 0600 permissions. The file holds instance
// URLs and identities — not secrets, but not public either.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o600)
}

// Names lists configured profiles.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolveName picks the active profile: an explicit flag, then NPMCTL_PROFILE,
// then the configured default, then "default".
func (c *Config) ResolveName(flag, env string) string {
	if s := strings.TrimSpace(flag); s != "" {
		return s
	}
	if s := strings.TrimSpace(env); s != "" {
		return s
	}
	if s := strings.TrimSpace(c.DefaultProfile); s != "" {
		return s
	}
	return "default"
}

// Get returns a profile, creating an empty one in memory when it is unknown so
// that flag- and env-supplied URLs still work without a config file.
func (c *Config) Get(name string) *Profile {
	if p, ok := c.Profiles[name]; ok && p != nil {
		return p
	}
	return &Profile{}
}

// Upsert stores a profile and makes it the default when it is the only one.
func (c *Config) Upsert(name string, p *Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	c.Profiles[name] = p
	if c.DefaultProfile == "" || len(c.Profiles) == 1 {
		c.DefaultProfile = name
	}
}

// Describe renders a one-line summary for `auth status`.
func (p *Profile) Describe() string {
	s := p.URL
	if p.Identity != "" {
		s += fmt.Sprintf(" (%s)", p.Identity)
	}
	if p.InsecureSkipVerify {
		s += " [TLS VERIFICATION DISABLED]"
	}
	return s
}
