package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bundle "github.com/tqer39/dev-standards"
)

func TestClaudeRuleAdoptionAcrossProfiles(t *testing.T) {
	fixtures := map[string]func(*testing.T) (*Tree, Request){
		"go": func(t *testing.T) (*Tree, Request) {
			return fixture(t), Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}
		},
		"node-rulesync": nodeFixture,
		"python":        pythonFixture,
		"mise-project":  projectFixture,
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			tree, req := fixture(t)
			write(t, tree.Root, "CLAUDE.md", "# Existing instructions\n")
			write(t, tree.Root, ".claude/rules/personal.md", "# Existing rule\n")
			// rulesync already uses this name in real adopted repositories.
			write(t, tree.Root, ".claude/rules/dev-standards.md", "# Existing rulesync output\n")
			if name == "mise-project" {
				req.InstructionLanguage = "en"
			}
			p, err := tree.Plan(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := tree.Apply(p); err != nil {
				t.Fatal(err)
			}
			rule := read(t, tree.Root, claudeRulePath)
			if !strings.Contains(rule, "agent-harness run") || !strings.Contains(rule, "agent-harness doctor") {
				t.Fatal("Claude cannot reach the harness from project rules")
			}
			if name == "mise-project" && !strings.HasPrefix(rule, "# dev-standards: implementation delegation") {
				t.Fatal("English instruction language ignored")
			}
			if read(t, tree.Root, "CLAUDE.md") != "# Existing instructions\n" || read(t, tree.Root, ".claude/rules/personal.md") != "# Existing rule\n" {
				t.Fatal("unrelated instructions overwritten")
			}
			if read(t, tree.Root, ".claude/rules/dev-standards.md") != "# Existing rulesync output\n" {
				t.Fatal("existing rulesync output overwritten")
			}
			var state State
			if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
				t.Fatal(err)
			}
			if !state.Files[claudeRulePath].Exists {
				t.Fatal("Claude rule not owned")
			}
			commit(t, tree.Root)
			again, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
				t.Fatalf("reapply: %+v %v", again, err)
			}
			if req.Automation && !strings.Contains(read(t, tree.Root, ".github/workflows/dev-standards.yml"), "'"+claudeRulePath+"'") {
				t.Fatal("CI ownership check omits Claude rule")
			}
			write(t, tree.Root, claudeRulePath, rule+"\nHuman edit.\n")
			conflict, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(conflict.Conflicts) != 1 {
				t.Fatalf("conflict: %+v %v", conflict, err)
			}
			if err := tree.Apply(conflict); err == nil {
				t.Fatal("overwrote modified rule")
			}
			if read(t, tree.Root, claudeRulePath) != rule+"\nHuman edit.\n" {
				t.Fatal("human edit lost")
			}
		})
	}
}

func TestClaudeRuleRejectsUnownedFilesAndStalePlans(t *testing.T) {
	for _, name := range []string{"existing", "after-plan", "ignored"} {
		t.Run(name, func(t *testing.T) {
			tree := fixture(t)
			req := Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}
			if name == "existing" {
				write(t, tree.Root, claudeRulePath, "# Personal rule\n")
			}
			if name == "ignored" {
				write(t, tree.Root, ".gitignore", ".claude/\n")
			}
			p, err := tree.Plan(req)
			if name != "after-plan" {
				if err == nil {
					t.Fatal("unsafe adoption accepted")
				}
				if name == "existing" && read(t, tree.Root, claudeRulePath) != "# Personal rule\n" {
					t.Fatal("existing rule lost")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			write(t, tree.Root, claudeRulePath, "# Concurrent rule\n")
			if err := tree.Apply(p); err == nil {
				t.Fatal("stale plan overwrote concurrent rule")
			}
			if read(t, tree.Root, claudeRulePath) != "# Concurrent rule\n" {
				t.Fatal("concurrent rule lost")
			}
		})
	}
}

func TestClaudeRuleAdoptionRollback(t *testing.T) {
	tree := fixture(t)
	p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("write failure after rule")
	ruleWritten := false
	err = tree.apply(p, func(i int) error {
		if i > 0 && p.Changes[i-1].Path == claudeRulePath {
			ruleWritten = true
			return injected
		}
		return nil
	})
	if !ruleWritten || !errors.Is(err, injected) {
		t.Fatalf("failure not injected after Claude rule: %v", err)
	}
	for _, name := range []string{claudeRulePath, ".claude", ".standards/state.json"} {
		if _, err := os.Stat(filepath.Join(tree.Root, name)); !os.IsNotExist(err) {
			t.Fatalf("rollback left %s: %v", name, err)
		}
	}
	if err := tree.Apply(p); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeRuleUpgradeFromEarlierOwnership(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "unowned"}[existing], func(t *testing.T) {
			tree := fixture(t)
			adopt(t, tree)
			var state State
			if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
				t.Fatal(err)
			}
			delete(state.Files, claudeRulePath)
			state.Bundle = strings.Repeat("a", 64)
			write(t, tree.Root, ".standards/state.json", string(JSON(state)))
			var lock Lock
			if err := strictJSON(read(t, tree.Root, ".standards/lock.json"), &lock); err != nil {
				t.Fatal(err)
			}
			lock.Bundle = state.Bundle
			write(t, tree.Root, ".standards/lock.json", string(JSON(lock)))
			if existing {
				write(t, tree.Root, claudeRulePath, "# Personal rule\n")
			} else if err := os.Remove(filepath.Join(tree.Root, claudeRulePath)); err != nil {
				t.Fatal(err)
			}
			commit(t, tree.Root)
			p, err := tree.Plan(Request{Mode: "upgrade"})
			if existing {
				if err == nil || read(t, tree.Root, claudeRulePath) != "# Personal rule\n" {
					t.Fatalf("unowned rule accepted or changed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tree.Apply(p); err != nil {
				t.Fatal(err)
			}
			if read(t, tree.Root, claudeRulePath) != bundle.Read("templates/base/claude-agent-harness.md") {
				t.Fatal("upgrade did not deliver the rule")
			}
			if _, err := tree.Check(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
