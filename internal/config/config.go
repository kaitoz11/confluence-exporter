// Package config remembers the user's settings between runs.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config is saved as JSON in the user's config folder, readable only by them.
type Config struct {
	URL         string `json:"url"`
	Email       string `json:"email"`
	Token       string `json:"token,omitempty"` // only when the user ticks "remember"
	OutputDir   string `json:"output_dir"`
	Attachments *bool  `json:"attachments,omitempty"`
	PDF         *bool  `json:"pdf,omitempty"`
	Website     *bool  `json:"website,omitempty"`
	Speed       string `json:"speed,omitempty"` // Gentle, Normal or Fast
	LastPDFDir  string `json:"last_pdf_dir,omitempty"`
}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "confluence-exporter", "config.json"), nil
}

// Load returns the saved settings, or defaults on first run. Environment
// variables CONFLUENCE_URL, CONFLUENCE_EMAIL and CONFLUENCE_API_TOKEN take
// precedence, which is handy for IT staff preparing a machine.
func Load() Config {
	var c Config
	if p, err := path(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &c)
		}
	}
	for env, field := range map[string]*string{
		"CONFLUENCE_URL":       &c.URL,
		"CONFLUENCE_EMAIL":     &c.Email,
		"CONFLUENCE_API_TOKEN": &c.Token,
	} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			*field = v
		}
	}
	if c.OutputDir == "" {
		c.OutputDir = DefaultOutputDir()
	}
	return c
}

// Save writes the settings with owner-only permissions.
func Save(c Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// DefaultOutputDir is a folder in Documents (or the home folder).
func DefaultOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "Confluence Export"
	}
	for _, d := range []string{"Documents", "documents"} {
		if fi, err := os.Stat(filepath.Join(home, d)); err == nil && fi.IsDir() {
			return filepath.Join(home, d, "Confluence Export")
		}
	}
	return filepath.Join(home, "Confluence Export")
}

// ExpandPath cleans up a path typed, pasted or dragged into the terminal:
// surrounding quotes, backslash-escaped spaces and a leading ~.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && (p[0] == '\'' || p[0] == '"') && p[len(p)-1] == p[0] {
		p = p[1 : len(p)-1]
	}
	if filepath.Separator == '/' {
		p = strings.ReplaceAll(p, `\ `, " ")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if p == "" {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
