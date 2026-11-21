package engine

import (
	"encoding/json"
	"strings"
	"testing"

	bundle "github.com/tqer39/dev-standards"
)

func claudeHookCount(t *testing.T, content string) int {
	t.Helper()
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(content), &settings); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		for _, group := range settings.Hooks[event] {
			for _, hook := range group.Hooks {
				if hook.Command == hookCommand {
					n++
				}
			}
		}
	}
	return n
}

func TestClaudeCodeTargetAdoption(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, "CLAUDE.md", "# Claude 固有\n\n既存の Claude 指示。\n")
	write(t, tree.Root, ".claude/settings.json", `{"permissions":{"allow":["Bash(ls)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo original"}]}]}}`)
	p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true, AITargets: []string{"claudecode"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	claude := read(t, tree.Root, "CLAUDE.md")
	if !strings.Contains(claude, "既存の Claude 指示") || !strings.Contains(claude, "mise run standards:prepare") {
		t.Fatalf("CLAUDE.md lacks local or shared entry: %s", claude)
	}
	settings := read(t, tree.Root, ".claude/settings.json")
	if !strings.Contains(settings, "Bash(ls)") || !strings.Contains(settings, "echo original") || strings.Contains(settings, "commandWindows") {
		t.Fatalf("settings lost existing content or carries Codex-only keys: %s", settings)
	}
	if n := claudeHookCount(t, settings); n != 3 {
		t.Fatalf("expected 3 dev-standards hooks, got %d", n)
	}
	if _, err := tree.Read(".codex/hooks.json"); err != nil {
		t.Fatal(err)
	}
	if s, _ := tree.Read(".codex/hooks.json"); s.Exists {
		t.Fatal("codex hooks created without codexcli target")
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if !strings.Contains(workflow, "'AGENTS.md', 'CLAUDE.md', '.claude/settings.json', '.standards/rules.md'") {
		t.Fatalf("workflow ownership set does not match claudecode outputs")
	}
	var state State
	if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE.md", ".claude/settings.json"} {
		if !state.Files[name].Exists {
			t.Fatalf("%s not owned", name)
		}
	}
	if !strings.Contains(read(t, tree.Root, ".gitattributes"), "/.claude/settings.json text eol=lf") {
		t.Fatal("settings line endings not pinned")
	}
	again, err := tree.Plan(Request{Mode: "upgrade", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("reapply drifted: %+v %v", again.Changes, again.Conflicts)
	}
	var m Manifest
	if err = bundle.Decode([]byte(read(t, tree.Root, ".standards/repository.yml")), "repository", &m); err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.AI.Targets, ",") != "claudecode" {
		t.Fatalf("sync overwrote targets: %v", m.AI.Targets)
	}
	write(t, tree.Root, ".claude/settings.json", "{}\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if len(conflict.Conflicts) == 0 {
		t.Fatal("direct edit of managed settings was not reported")
	}
}

func TestSwitchTargetsFromCodexToClaudeCode(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, ".codex/hooks.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo codex"}]}]}}`)
	p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	manifest := read(t, tree.Root, ".standards/repository.yml")
	write(t, tree.Root, ".standards/repository.yml", strings.Replace(manifest, `"codexcli"`, `"claudecode"`, 1))
	p, err = tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) != 0 {
		t.Fatal(p.Conflicts)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	// The pre-adoption Codex hooks come back as an unowned file.
	if got := read(t, tree.Root, ".codex/hooks.json"); !strings.Contains(got, "echo codex") || strings.Contains(got, "dev-standards") {
		t.Fatalf("codex hooks not restored: %s", got)
	}
	var state State
	if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	if _, owned := state.Files[".codex/hooks.json"]; owned || !state.Files["CLAUDE.md"].Exists || !state.Files[".claude/settings.json"].Exists {
		t.Fatalf("ownership not moved: %v", state.Files)
	}
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("second reapply drifted: %+v %v", again.Changes, again.Conflicts)
	}
	write(t, tree.Root, ".standards/repository.yml", strings.Replace(read(t, tree.Root, ".standards/repository.yml"), `"claudecode"`, `"codexcli"`, 1))
	p, err = tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	// No CLAUDE.md or settings existed before, so switching back removes them.
	for _, name := range []string{"CLAUDE.md", ".claude/settings.json"} {
		if s, _ := tree.Read(name); s.Exists {
			t.Fatalf("%s left behind", name)
		}
	}
}

func TestClaudeCodeTargetWithRulesyncKeepsCLAUDEUnmanaged(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, "AGENTS.md", "# generated\n")
	p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Instructions: "rulesync", Automation: true, AITargets: []string{"codexcli", "claudecode"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if c.Path == "CLAUDE.md" {
			t.Fatal("rulesync owns CLAUDE.md generation")
		}
	}
	if err = tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if !strings.Contains(workflow, "'"+rulesyncEntryPath+"', '.codex/hooks.json', '.claude/settings.json'") {
		t.Fatal("workflow ownership set wrong for rulesync")
	}
}

func TestInvalidAITargets(t *testing.T) {
	for _, targets := range [][]string{{"cursor"}, {"claudecode", "claudecode"}} {
		tree := fixture(t)
		if _, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true, AITargets: targets}); err == nil {
			t.Fatalf("accepted %v", targets)
		}
	}
	tree := fixture(t)
	if _, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", AITargets: []string{"claudecode"}}); err == nil {
		t.Fatal("accepted targets without automation")
	}
}
