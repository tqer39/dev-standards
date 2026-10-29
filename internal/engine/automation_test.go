package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	bundle "github.com/tqer39/dev-standards"
)

func TestAutomationAdoptionAndStableHooks(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, "AGENTS.md", "# 固有ルール\n\n既存の指示を維持する。\n")
	write(t, tree.Root, ".codex/hooks.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo original"}]}]}}`)
	p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, tree.Root, "AGENTS.md"), "既存の指示") {
		t.Fatal("lost original instructions")
	}
	var taskConfig map[string]struct {
		Run        string
		RunWindows string `toml:"run_windows"`
	}
	if _, err := toml.Decode(read(t, tree.Root, ".standards/tasks.standards.toml"), &taskConfig); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"format:check", "standards:check", "standards:plan", "standards:verify", "standards:prepare", "standards:finish"} {
		if !strings.HasPrefix(taskConfig[name].RunWindows, `.\.standards\cache\dev-standards.exe `) {
			t.Fatalf("Windows launcher missing for %s: %+v", name, taskConfig[name])
		}
	}
	original := read(t, tree.Root, ".codex/hooks.json")
	if !strings.Contains(original, "echo original") {
		t.Fatal("lost original hook")
	}
	var m Manifest
	if err = bundle.Decode([]byte(read(t, tree.Root, ".standards/repository.yml")), "repository", &m); err != nil {
		t.Fatal(err)
	}
	if len(m.AI.Targets) != 1 {
		t.Fatal("adapter not declared")
	}
	again, err := tree.Plan(Request{Mode: "upgrade", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 0 {
		t.Fatalf("repeated sync changed files: %+v", again.Changes)
	}
	if err = tree.Apply(again); err != nil {
		t.Fatal(err)
	}
	if read(t, tree.Root, ".codex/hooks.json") != original {
		t.Fatal("hook definition changed")
	}
}
func TestUpgradeFromPreviousBundle(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	var lock Lock
	if err := strictJSON(read(t, tree.Root, ".standards/lock.json"), &lock); err != nil {
		t.Fatal(err)
	}
	lock.Version = "0.1.0"
	lock.Bundle = strings.Repeat("a", 64)
	write(t, tree.Root, ".standards/lock.json", string(JSON(lock)))
	var state State
	if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	state.Bundle = lock.Bundle
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	var m Manifest
	if err := bundle.Decode([]byte(read(t, tree.Root, ".standards/repository.yml")), "repository", &m); err != nil {
		t.Fatal(err)
	}
	m.Standard.Version = "0.1.0"
	write(t, tree.Root, ".standards/repository.yml", string(JSON(m)))
	if _, err := tree.Plan(Request{Mode: "reapply"}); err == nil {
		t.Fatal("silent upgrade allowed")
	}
	p, err := tree.Plan(Request{Mode: "upgrade", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	if _, err = tree.Check(); err != nil {
		t.Fatal(err)
	}
}
func TestReceiptInvalidatesOnCodeChange(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	fingerprint, err := tree.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.WriteReceipt(fingerprint); err != nil {
		t.Fatal(err)
	}
	if !tree.ReceiptValid() {
		t.Fatal("valid receipt rejected")
	}
	write(t, tree.Root, "main.go", "package main\n\nfunc main() { println(1) }\n")
	if tree.ReceiptValid() {
		t.Fatal("receipt reused after edit")
	}
	if err = tree.WriteReceipt(fingerprint); err == nil {
		t.Fatal("stale verification accepted")
	}
}
func TestUnknownStateOwnershipCannotBeMigrated(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	var state State
	_ = json.Unmarshal([]byte(read(t, tree.Root, ".standards/state.json")), &state)
	state.Files["go.mod"] = file("module stolen")
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	if _, err := tree.Plan(Request{Mode: "upgrade"}); err == nil {
		t.Fatal("unknown previous ownership accepted")
	}
}
func TestAutomationSymlinkRejected(t *testing.T) {
	tree := fixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(tree.Root, ".codex")); err != nil {
		t.Skip(err)
	}
	if _, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true}); err == nil {
		t.Fatal("hook path escape accepted")
	}
}

func TestUpgradeNeverDowngradesOrDropsOwnership(t *testing.T) {
	tree := fixture(t)
	p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	var state State
	if err = strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	delete(state.Files, "mise.toml")
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	if _, err = tree.Plan(Request{Mode: "upgrade", Automation: true}); err == nil || !strings.Contains(err.Error(), "required ownership") {
		t.Fatalf("missing ownership: %v", err)
	}
	for _, tc := range []struct {
		old, current string
		want         bool
	}{
		{"0.1.0", "0.2.0", true}, {"0.2.0", "0.2.0", true}, {"1.0.0", "0.2.0", false}, {"0.2.1", "0.2.0", false}, {"bad", "0.2.0", false},
	} {
		if nonDowngrade(tc.old, tc.current) != tc.want {
			t.Fatalf("version ordering: %+v", tc)
		}
	}
}
