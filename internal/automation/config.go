package automation

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Entry struct {
	Path   string `json:"path"`
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	Role   string `json:"role"`
	Canary bool   `json:"canary"`
}
type Config struct {
	SchemaVersion  int     `json:"schemaVersion"`
	Source         Entry   `json:"source"`
	Projects       []Entry `json:"projects"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
	MaxFiles       int     `json:"maxFiles"`
	MaxLines       int     `json:"maxLines"`
	MaxAttempts    int     `json:"maxAttempts"`
}

func Default(source Entry) Config { return Config{1, source, []Entry{}, 1800, 32, 1200, 2} }
func Load(name string) (Config, error) {
	var c Config
	f, err := os.Open(name)
	if err != nil {
		return c, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("one fleet document is required")
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (c Config) Validate() error {
	if c.SchemaVersion != 1 || c.TimeoutSeconds < 30 || c.TimeoutSeconds > 3600 || c.MaxFiles < 1 || c.MaxFiles > 100 || c.MaxLines < 1 || c.MaxLines > 5000 || c.MaxAttempts < 1 || c.MaxAttempts > 3 {
		return fmt.Errorf("invalid maintenance limits")
	}
	entries := append([]Entry{c.Source}, c.Projects...)
	seen := map[string]bool{}
	for _, e := range entries {
		if !filepath.IsAbs(e.Path) || e.Remote == "" || e.Branch == "" || strings.ContainsAny(e.Branch, " \r\n:~^?*[\\") || strings.HasPrefix(e.Branch, "-") {
			return fmt.Errorf("invalid fleet entry")
		}
		if e.Role != "source" && e.Role != "project" {
			return fmt.Errorf("unknown fleet role")
		}
		if seen[e.Path] {
			return fmt.Errorf("duplicate fleet entry")
		}
		seen[e.Path] = true
		if !strings.HasPrefix(e.Remote, "https://github.com/") && !filepath.IsAbs(e.Remote) {
			return fmt.Errorf("use an explicit GitHub HTTPS or local test remote")
		}
		if strings.ContainsAny(e.Remote, "\r\n\x00") || strings.Contains(e.Remote, "@") {
			return fmt.Errorf("remote must not contain credentials")
		}
	}
	if c.Source.Role != "source" {
		return fmt.Errorf("source role is required")
	}
	return nil
}
func Save(name string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}
