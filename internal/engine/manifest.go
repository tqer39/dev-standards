package engine

import (
	"fmt"
	"path"
	"strings"

	bundle "github.com/tqer39/dev-standards"
)

func defaultManifest() Manifest {
	var m Manifest
	m.SchemaVersion = 1
	m.Standard.Repository = bundle.Repository
	m.Standard.Version = bundle.Version
	m.Platforms = []string{"macos", "linux", "windows"}
	m.Components = []Component{{"main", ".", "go"}}
	m.AI.Targets = []string{}
	m.Gitignore.Templates = []string{"go", "linux", "macos", "windows"}
	m.Gitignore.Local = ".standards/gitignore.local"
	m.Update.Channel = "stable"
	m.Update.Rollout = "manual"
	m.CI.PRBudgetSeconds = 600
	m.CI.MinimumSamples = 20
	m.CI.BreachWindows = 2
	return m
}
func validateManifest(m Manifest) error {
	if err := bundle.Decode(JSON(m), "repository", nil); err != nil {
		return err
	}
	if m.Standard.Repository != bundle.Repository || m.Standard.Version != bundle.Version {
		return fmt.Errorf("this CLI only supports %s@%s", bundle.Repository, bundle.Version)
	}
	if len(m.Components) != 1 || m.Components[0].Path != "." || m.Components[0].Profile != "go" {
		return fmt.Errorf("initial release supports one Go component at the repository root")
	}
	if len(m.AI.Targets) != 0 {
		return fmt.Errorf("AI targets require the later rulesync integration; use an empty list")
	}
	if m.Gitignore.Local != ".standards/gitignore.local" {
		return fmt.Errorf("gitignore.local path is reserved")
	}
	if strings.Join(m.Gitignore.Templates, ",") != "go,linux,macos,windows" {
		return fmt.Errorf("this bundle contains the go,linux,macos,windows gitignore.io snapshot")
	}
	return nil
}
func languageDecision(s string) error {
	s = normalized(s)
	for _, heading := range []string{"## 要件", "## 候補", "## 選定理由"} {
		_, tail, ok := strings.Cut(s, heading+"\n")
		if !ok || len(strings.TrimSpace(strings.Split(tail, "\n## ")[0])) < 8 {
			return fmt.Errorf("language decision needs a substantive %q section", heading)
		}
	}
	if !strings.Contains(strings.ToLower(s), "go") {
		return fmt.Errorf("record the selected Go language")
	}
	return nil
}
func cleanLocalEditor(s string) (string, error) {
	var lines []string
	section := ""
	goIndent := "tab"
	for _, line := range strings.Split(normalized(s), "\n") {
		v := strings.TrimSpace(line)
		if v == "" || strings.HasPrefix(v, "#") || strings.HasPrefix(v, ";") {
			lines = append(lines, line)
			continue
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			section = v
			lines = append(lines, line)
			continue
		}
		if k, val, ok := strings.Cut(v, "="); ok {
			k = strings.TrimSpace(strings.ToLower(k))
			val = strings.TrimSpace(strings.ToLower(val))
			if k == "root" {
				if section != "" {
					return "", fmt.Errorf("EditorConfig root belongs before all sections")
				}
				if val != "true" {
					return "", fmt.Errorf("EditorConfig root must be true")
				}
				continue
			}
			if section == "" {
				return "", fmt.Errorf("EditorConfig properties require a section")
			}
			if k == "indent_style" && section == "[*]" {
				goIndent = val
			}
			if k == "indent_style" && (section == "[*.go]" || section == "[**.go]") {
				goIndent = val
			}
			if k == "charset" && val != "utf-8" || k == "end_of_line" && val != "lf" || k == "insert_final_newline" && val != "true" {
				return "", fmt.Errorf("local EditorConfig conflicts with required %s", k)
			}
			if k == "indent_style" && val != "tab" && section == "[*.go]" {
				return "", fmt.Errorf("Go indentation must use tabs; move other language overrides into specific sections")
			}
		} else {
			return "", fmt.Errorf("invalid EditorConfig line: %s", v)
		}
		lines = append(lines, line)
	}
	if goIndent != "tab" {
		return "", fmt.Errorf("local EditorConfig overrides Go tab indentation")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n", nil
}
func validateInputPath(p string) error {
	if err := validPath(p); err != nil {
		return err
	}
	if path.Ext(p) != ".md" {
		return fmt.Errorf("language decision input must be a Markdown file")
	}
	return nil
}
