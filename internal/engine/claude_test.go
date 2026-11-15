package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredClaudeRuleAcrossProfiles(t *testing.T) {
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
			assertRetiredRuleRemoved(t, tree)
			if read(t, tree.Root, "CLAUDE.md") != "# Existing instructions\n" || read(t, tree.Root, ".claude/rules/personal.md") != "# Existing rule\n" {
				t.Fatal("unrelated instructions overwritten")
			}
			commit(t, tree.Root)
			again, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
				t.Fatalf("reapply: %+v %v", again, err)
			}
			if req.Automation && strings.Contains(read(t, tree.Root, ".github/workflows/dev-standards.yml"), claudeRulePath) {
				t.Fatal("CI still requires the retired rule")
			}
		})
	}
}

func ownRetiredRule(t *testing.T, tree *Tree, upgrading bool) {
	t.Helper()
	content := "# Previously distributed rule\n"
	write(t, tree.Root, claudeRulePath, content)
	var state State
	if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	state.Files[claudeRulePath] = file(content)
	if upgrading {
		state.Bundle = strings.Repeat("a", 64)
		var lock Lock
		if err := strictJSON(read(t, tree.Root, ".standards/lock.json"), &lock); err != nil {
			t.Fatal(err)
		}
		lock.Bundle = state.Bundle
		write(t, tree.Root, ".standards/lock.json", string(JSON(lock)))
	}
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	commit(t, tree.Root)
}

func assertRetiredRuleRemoved(t *testing.T, tree *Tree) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(tree.Root, claudeRulePath)); !os.IsNotExist(err) {
		t.Fatalf("retired rule remains: %v", err)
	}
	var state State
	if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Files[claudeRulePath]; exists {
		t.Fatal("retired rule ownership remains")
	}
}

func TestRetiredClaudeRuleMigration(t *testing.T) {
	for _, mode := range []string{"reapply", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			tree := fixture(t)
			adopt(t, tree)
			ownRetiredRule(t, tree, mode == "upgrade")
			p, err := tree.Plan(Request{Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, change := range p.Changes {
				if change.Path == claudeRulePath {
					found = change.Before.Exists && !change.After.Exists && change.After.Mode == 0
				}
			}
			if !found {
				t.Fatal("owned rule deletion not planned")
			}
			if err := tree.Apply(p); err != nil {
				t.Fatal(err)
			}
			assertRetiredRuleRemoved(t, tree)
			commit(t, tree.Root)
			again, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(again.Changes) != 0 {
				t.Fatalf("migration not idempotent: %+v %v", again, err)
			}
			if _, err := tree.Check(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRetiredClaudeRulePreservesUnowned(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "adopt", false: "reapply"}[initial], func(t *testing.T) {
			tree := fixture(t)
			req := Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}
			if !initial {
				adopt(t, tree)
				req = Request{Mode: "reapply"}
			}
			write(t, tree.Root, claudeRulePath, "# Personal rule\n")
			p, err := tree.Plan(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := tree.Apply(p); err != nil {
				t.Fatal(err)
			}
			if read(t, tree.Root, claudeRulePath) != "# Personal rule\n" {
				t.Fatal("unowned rule changed")
			}
			var state State
			if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
				t.Fatal(err)
			}
			if _, exists := state.Files[claudeRulePath]; exists {
				t.Fatal("unowned rule claimed")
			}
		})
	}
}

func TestRetiredClaudeRulePreservesEditsAndRejectsStalePlans(t *testing.T) {
	for _, afterPlan := range []bool{true, false} {
		t.Run(map[bool]string{true: "stale-plan", false: "edited"}[afterPlan], func(t *testing.T) {
			tree := fixture(t)
			adopt(t, tree)
			ownRetiredRule(t, tree, false)
			p, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil {
				t.Fatal(err)
			}
			write(t, tree.Root, claudeRulePath, "# Human edit\n")
			if !afterPlan {
				p, err = tree.Plan(Request{Mode: "reapply"})
				if err != nil || len(p.Conflicts) != 1 {
					t.Fatalf("missing conflict: %+v %v", p, err)
				}
			}
			if err := tree.Apply(p); err == nil {
				t.Fatal("deleted edited rule")
			}
			if read(t, tree.Root, claudeRulePath) != "# Human edit\n" {
				t.Fatal("human edit lost")
			}
		})
	}
}

func TestRetiredClaudeRuleMigrationRollback(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	ownRetiredRule(t, tree, false)
	originalState := read(t, tree.Root, ".standards/state.json")
	p, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("write failure after deletion")
	deleted := false
	err = tree.apply(p, func(i int) error {
		if i > 0 && p.Changes[i-1].Path == claudeRulePath {
			deleted = true
			return injected
		}
		return nil
	})
	if !deleted || !errors.Is(err, injected) {
		t.Fatalf("failure not injected: %v", err)
	}
	if read(t, tree.Root, claudeRulePath) != "# Previously distributed rule\n" || read(t, tree.Root, ".standards/state.json") != originalState {
		t.Fatal("rollback did not restore rule and ownership")
	}
	if err := tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	assertRetiredRuleRemoved(t, tree)
}

func TestRetiredClaudeRuleAlreadyDeleted(t *testing.T) {
	for _, committed := range []bool{true, false} {
		t.Run(map[bool]string{true: "committed", false: "uncommitted"}[committed], func(t *testing.T) {
			tree := fixture(t)
			adopt(t, tree)
			ownRetiredRule(t, tree, false)
			if err := os.Remove(filepath.Join(tree.Root, claudeRulePath)); err != nil {
				t.Fatal(err)
			}
			if committed {
				commit(t, tree.Root)
			}
			p, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil {
				t.Fatal(err)
			}
			if !committed {
				if len(p.Conflicts) != 1 {
					t.Fatalf("uncommitted deletion lost: %+v", p)
				}
				return
			}
			if err := tree.Apply(p); err != nil {
				t.Fatal(err)
			}
			assertRetiredRuleRemoved(t, tree)
		})
	}
}
