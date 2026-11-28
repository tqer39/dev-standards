package engine

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// Projects lint the distributed rules with their own markdownlint, textlint and
// cspell settings, so the document must not depend on an exclusion on their side.
func TestDistributedRulesPassProjectLinters(t *testing.T) {
	rules := read(t, adoptWithAutomation(t).Root, ".standards/rules.md")
	headings := map[string]bool{}
	fenced := false
	for i, line := range strings.Split(rules, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(line, "#") {
			// markdownlint MD024 compares the heading text without its level.
			text := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if headings[text] {
				t.Fatalf("line %d repeats the heading %q", i+1, text)
			}
			headings[text] = true
		}
		// textlint no-mix-dearu-desumasu rejects this ending where the project prefers the polite style.
		if strings.Contains(line, "である。") {
			t.Fatalf("line %d ends a sentence with である: %s", i+1, line)
		}
	}
	if !headings["配布と検証の範囲（PROCESS-WORKTREE）"] {
		t.Fatal("section headings lack the rule ID")
	}
	// Dictionaries lack the zizmor audit name, so the document declares it for cspell.
	if !strings.Contains(rules, "<!-- cspell:words artipacked zizmor betterleaks -->") {
		t.Fatal("zizmor audit name is not declared for cspell")
	}
}

// Projects add no words for the distributed rules, so every word outside the
// dictionaries bundled with cspell must be declared in the guidance itself.
func TestDistributedRulesPassCspellWithoutProjectWords(t *testing.T) {
	cspell, err := exec.LookPath("cspell")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("cspell is not installed")
		}
		t.Skip("cspell is not installed; mise installs it")
	}
	dir := t.TempDir()
	write(t, dir, "rules.md", read(t, adoptWithAutomation(t).Root, ".standards/rules.md"))
	write(t, dir, "cspell.json", `{"version": "0.2", "language": "en"}`+"\n")
	cmd := exec.Command(cspell, "--no-progress", "--config", "cspell.json", "rules.md")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("declare the words with cspell:words in the guidance: %v\n%s", err, out)
	}
}

// A betterleaks configuration without extend.useDefault replaces the default
// rules, so an example that only lists exclusions would disable the scan.
func TestSecretExclusionExampleKeepsDefaultRules(t *testing.T) {
	rules := read(t, adoptWithAutomation(t).Root, ".standards/rules.md")
	_, rest, found := strings.Cut(rules, "```toml\n")
	example, _, closed := strings.Cut(rest, "```\n")
	if !found || !closed || !strings.Contains(example, "[[allowlists]]") {
		t.Fatal("exclusion example is missing")
	}
	var config struct {
		Extend struct {
			UseDefault bool `toml:"useDefault"`
		} `toml:"extend"`
		Allowlists []struct {
			Paths []string `toml:"paths"`
		} `toml:"allowlists"`
	}
	if _, err := toml.Decode(example, &config); err != nil {
		t.Fatal(err)
	}
	if !config.Extend.UseDefault || len(config.Allowlists) != 1 || len(config.Allowlists[0].Paths) != 1 {
		t.Fatalf("exclusion example disables the default rules: %+v", config)
	}
}
