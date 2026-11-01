package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nodePackage = `{"name":"sample","private":true,"scripts":{"lint":"oxlint .","format":"oxfmt --write .","format:check":"oxfmt --check .","test":"node --test","build":"tsc"}}`

func nodeFixture(t *testing.T) (*Tree, Request) {
	t.Helper()
	tree := fixture(t)
	for _, name := range []string{"go.mod", "main.go"} {
		if err := os.Remove(filepath.Join(tree.Root, name)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, tree.Root, "decision.md", "# 開発言語の選定\n\n## 要件\n\nブラウザーで動く単一 HTML のビューアーを作る。\n\n## 候補\n\nTypeScript と Go を、描画ライブラリの充実度から比較する。\n\n## 選定理由\n\nTypeScript を採用する。ブラウザーの描画とエディターを同じ言語で書けるため。\n")
	write(t, tree.Root, "package.json", nodePackage)
	write(t, tree.Root, "package-lock.json", `{"name":"sample","lockfileVersion":3,"packages":{}}`)
	write(t, tree.Root, "mise.toml", "[tools]\nnode = \"24.21.0\"\npython = \"3.15.0\"\n[tasks.dev]\nrun = \"vite\"\n")
	write(t, tree.Root, ".gitignore", "node_modules/\ndist/\n")
	write(t, tree.Root, "AGENTS.md", "# 固有ルール\n\nrulesync が生成する。\n")
	return tree, Request{Mode: "adopt", Profile: "node-npm", DecisionPath: "decision.md", Instructions: "rulesync", Automation: true}
}

func TestNodeAdoptWithRulesyncInstructions(t *testing.T) {
	tree, req := nodeFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if got := read(t, tree.Root, "AGENTS.md"); got != "# 固有ルール\n\nrulesync が生成する。\n" {
		t.Fatalf("rulesync output was rewritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(tree.Root, ".standards/agents.local.md")); !os.IsNotExist(err) {
		t.Fatal("AGENTS.md was imported although rulesync owns it")
	}
	entry := read(t, tree.Root, rulesyncEntryPath)
	if !strings.HasPrefix(entry, "---\nroot: false\n") || !strings.HasSuffix(entry, "---\n\n"+agentsEntry) {
		t.Fatalf("rulesync entry: %q", entry)
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("non-idempotent: %+v %v", again, err)
	}
	// Until rulesync regenerates AGENTS.md, the shared entry is not delivered.
	if _, err := tree.Check(); err == nil {
		t.Fatal("accepted AGENTS.md without the shared entry")
	}
	write(t, tree.Root, "AGENTS.md", "# 固有ルール\n\nrulesync が生成する。\n\n"+agentsEntry)
	if _, err := tree.Check(); err != nil {
		t.Fatal(err)
	}
	tasks := read(t, tree.Root, ".standards/tasks.standards.toml")
	for _, expected := range []string{"npm --prefix . ci --ignore-scripts", "npm --prefix . run lint", "npm --prefix . run format:check", "npm --prefix . run build"} {
		if !strings.Contains(tasks, expected) {
			t.Errorf("missing task: %s", expected)
		}
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if strings.Contains(workflow, "gofmt") || !strings.Contains(workflow, "mise run bootstrap\n      - run: mise run lint") || strings.Contains(workflow, "'AGENTS.md'") || !strings.Contains(workflow, "'"+rulesyncEntryPath+"'") {
		t.Fatalf("workflow does not follow the profile: %s", workflow)
	}
	attributes := read(t, tree.Root, ".gitattributes")
	for _, expected := range []string{"*.ts text eol=lf", "package-lock.json text eol=lf", "/" + rulesyncEntryPath + " text eol=lf"} {
		if !strings.Contains(attributes, expected) {
			t.Errorf("missing attribute: %s", expected)
		}
	}
	if strings.Contains(attributes, "go.sum") {
		t.Error("Go attributes generated")
	}
	write(t, tree.Root, rulesyncEntryPath, "human edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("missing conflict: %+v %v", conflict, err)
	}
}

func TestNodeRejectsUnsafeOrUnsupportedInputs(t *testing.T) {
	for _, variant := range []string{"missing-lock", "old-lock", "missing-script", "workspaces", "floating-node", "missing-node", "nested-package", "go-module", "ignored-lock", "decision-language", "unknown-instructions", "unowned-entry"} {
		t.Run(variant, func(t *testing.T) {
			tree, req := nodeFixture(t)
			switch variant {
			case "missing-lock":
				os.Remove(filepath.Join(tree.Root, "package-lock.json"))
			case "old-lock":
				write(t, tree.Root, "package-lock.json", `{"lockfileVersion":1}`)
			case "missing-script":
				write(t, tree.Root, "package.json", strings.Replace(nodePackage, `"format:check":"oxfmt --check .",`, "", 1))
			case "workspaces":
				write(t, tree.Root, "package.json", strings.Replace(nodePackage, `"private":true,`, `"workspaces":["packages/*"],`, 1))
			case "floating-node":
				write(t, tree.Root, "mise.toml", "[tools]\nnode = \"24\"\n")
			case "missing-node":
				write(t, tree.Root, "mise.toml", "[tools]\npython = \"3.15.0\"\n")
			case "nested-package":
				write(t, tree.Root, "examples/package.json", "{}")
			case "go-module":
				write(t, tree.Root, "go.mod", "module extra\n")
			case "ignored-lock":
				write(t, tree.Root, ".gitignore", "package-lock.json\n")
			case "decision-language":
				write(t, tree.Root, "decision.md", strings.NewReplacer("TypeScript", "Rust").Replace(read(t, tree.Root, "decision.md")))
			case "unknown-instructions":
				req.Instructions = "custom"
			case "unowned-entry":
				write(t, tree.Root, rulesyncEntryPath, "---\nroot: false\n---\n")
			}
			if _, err := tree.Plan(req); err == nil {
				t.Fatal("accepted unsupported input")
			}
		})
	}
}

func TestNodeWithoutRulesyncOwnsAgents(t *testing.T) {
	tree, req := nodeFixture(t)
	req.Instructions = ""
	p, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, tree.Root, "AGENTS.md"), "mise run standards:prepare") {
		t.Fatal("shared entry missing from AGENTS.md")
	}
	if _, err := os.Stat(filepath.Join(tree.Root, rulesyncEntryPath)); !os.IsNotExist(err) {
		t.Fatal("rulesync entry generated without rulesync")
	}
	if _, err := tree.Check(); err != nil {
		t.Fatal(err)
	}
}
