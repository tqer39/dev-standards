package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if strings.Contains(workflow, "gofmt") || !strings.Contains(workflow, "npm ci --ignore-scripts --prefix .\n      - run: mise run lint") || strings.Contains(workflow, "'AGENTS.md'") || !strings.Contains(workflow, "'"+rulesyncEntryPath+"'") {
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

func TestNodeCIPreflightPreservesChecksAndOwnership(t *testing.T) {
	tree, req := nodeFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if got := read(t, tree.Root, versionLagWorkflowPath); got != versionLagWorkflow {
		t.Fatal("version-lag workflow changed without a configured preflight")
	}
	write(t, tree.Root, "scripts/security.mjs", "process.exit(0);\n")
	manifest := read(t, tree.Root, ".standards/repository.yml")
	write(t, tree.Root, ".standards/repository.yml", strings.Replace(manifest, `"prBudgetSeconds":`, `"nodePreflight": "scripts/security.mjs", "prBudgetSeconds":`, 1))
	plan, err = tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	for _, required := range []string{"runs-on: ubuntu-24.04", "persist-credentials: false", "npm ci --ignore-scripts --prefix .", "mise run lint", "mise run format:check", "mise run test", "mise run build"} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("missing %s", required)
		}
	}
	if at := strings.Index(workflow, "run: node scripts/security.mjs"); at < 0 || at > strings.Index(workflow, "uses: jdx/mise-action") {
		t.Fatal("preflight executes after dependency setup")
	}
	lag := read(t, tree.Root, versionLagWorkflowPath)
	gate := "run: node scripts/security.mjs"
	if at := strings.Index(lag, gate); strings.Count(lag, gate) != 1 || at < strings.Index(lag, "uses: actions/checkout@") || at > strings.Index(lag, "python3 - <<'PYTHON'") {
		t.Fatal("version-lag preflight must execute once, after checkout and before the comparison")
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("reapply: %+v %v", again, err)
	}
	write(t, tree.Root, ".github/workflows/dev-standards.yml", workflow+"# human edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("conflict: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("applied conflicting workflow")
	}
	if got := read(t, tree.Root, ".github/workflows/dev-standards.yml"); got != workflow+"# human edit\n" {
		t.Fatal("human edit lost")
	}
	for _, invalid := range []string{"missing.mjs", "../outside.mjs", "scripts/security.mjs;echo bad", "scripts/security.mjs\nrun: bad"} {
		changed := strings.Replace(manifest, `"prBudgetSeconds":`, `"nodePreflight": `+string(JSON(invalid))+`, "prBudgetSeconds":`, 1)
		write(t, tree.Root, ".standards/repository.yml", changed)
		if _, err := tree.Plan(Request{Mode: "reapply"}); err == nil {
			t.Fatalf("accepted invalid preflight %q", invalid)
		}
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

func TestRulesyncOrdinaryAdoption(t *testing.T) {
	tree, req := nodeFixture(t)
	req.Automation = false
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	entry := read(t, tree.Root, rulesyncEntryPath)
	if !strings.Contains(entry, "mise run standards:verify") {
		t.Fatal("ordinary adoption lacks working verification instructions")
	}
	for _, name := range []string{".codex/hooks.json", ".github/workflows/dev-standards.yml", ".standards/agents.local.md"} {
		if _, err := os.Stat(filepath.Join(tree.Root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected automation output: %s", name)
		}
	}
	if _, err := tree.Check(); err == nil {
		t.Fatal("missing rulesync generation accepted")
	}
	write(t, tree.Root, "AGENTS.md", "# 固有ルール\n\n"+instructionsEntry(Manifest{}))
	if _, err := tree.Check(); err != nil {
		t.Fatal(err)
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("not idempotent: %+v %v", again, err)
	}
	write(t, tree.Root, rulesyncEntryPath, "user edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) == 0 {
		t.Fatalf("missing ownership conflict: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("overwrote edited instruction source")
	}
	if got := read(t, tree.Root, rulesyncEntryPath); got != "user edit\n" {
		t.Fatal("failed apply changed source")
	}
}

func TestNodeWorkflowRetainsBootstrapExtensions(t *testing.T) {
	tree, req := nodeFixture(t)
	write(t, tree.Root, ".standards/tasks.project.toml", "[\"project:bootstrap\"]\nrun = \"echo prepare additional tools\"\n")
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if !strings.Contains(workflow, "npm ci --ignore-scripts --prefix .\n      - run: mise run project:bootstrap\n      - run: mise run lint") {
		t.Fatal("bootstrap extension missing from CI")
	}
}

func TestNodeBootstrapExtensionExecution(t *testing.T) {
	if _, err := exec.LookPath("mise"); err != nil {
		t.Skip("mise is required for task graph execution")
	}
	tree, req := nodeFixture(t)
	write(t, tree.Root, "mise.toml", "[tools]\nnode = \"24.21.0\"\n")
	script := "const fs=require('node:fs');const phase=process.argv[1];const log=process.env.NODE_BOOTSTRAP_LOG;const lf=String.fromCharCode(10);" +
		"if(phase==='extension'){if(!fs.existsSync(log))process.exit(7);if(!fs.readFileSync(log,'utf8').endsWith('install'+lf))process.exit(8);}" +
		"fs.appendFileSync(log,phase+lf);if(phase==='install'){if(process.env.NODE_BOOTSTRAP_FAIL==='1')process.exit(3);}"
	command := "node -e " + strings.TrimSpace(string(JSON(script))) + " -- "
	extension := fmt.Sprintf("[\"project:bootstrap\"]\nrun = %q\n", command+"extension")
	write(t, tree.Root, ".standards/tasks.project.toml", extension)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	// Replace only the external installer; execute the generated graph with real Mise.
	tasks := read(t, tree.Root, ".standards/tasks.standards.toml")
	tasks = strings.Replace(tasks, `"npm --prefix . ci --ignore-scripts"`, fmt.Sprintf("%q", command+"install"), 1)
	write(t, tree.Root, ".standards/tasks.standards.toml", tasks)
	log := filepath.Join(t.TempDir(), "execution.log")
	run := func(fail string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "mise", "run", "bootstrap")
		cmd.Dir = tree.Root
		cmd.Env = append(os.Environ(), "MISE_TRUSTED_CONFIG_PATHS="+tree.Root, "MISE_YES=1", "MISE_ACTIVATE_AGGRESSIVE=true", "NODE_BOOTSTRAP_LOG="+log, "NODE_BOOTSTRAP_FAIL="+fail)
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("bootstrap did not terminate: %s", output)
		}
		return string(output), err
	}
	for i := 1; i <= 2; i++ {
		if output, err := run(""); err != nil {
			t.Fatalf("bootstrap %d: %v\n%s", i, err, output)
		}
		if data, err := os.ReadFile(log); err != nil || string(data) != strings.Repeat("install\nextension\n", i) {
			t.Fatalf("extension ran before installation: %q %v", data, err)
		}
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if output, err := run("1"); err == nil {
		t.Fatalf("accepted failed installation: %s", output)
	}
	if data, err := os.ReadFile(log); err != nil || string(data) != "install\n" {
		t.Fatalf("extension ran after failure: %q %v", data, err)
	}
	// A post-task may refer to its parent, but execution must stay in one graph.
	_ = os.Remove(log)
	write(t, tree.Root, ".standards/tasks.project.toml", extension+"depends = [\"bootstrap\"]\n")
	output, err := run("")
	if err != nil && !strings.Contains(strings.ToLower(output), "circular") {
		t.Fatalf("unexpected back-edge failure: %v\n%s", err, output)
	}
	data, _ := os.ReadFile(log)
	if strings.Count(string(data), "install\n") > 2 || strings.Count(string(data), "extension\n") > 1 {
		t.Fatalf("bootstrap recursively reentered: %q", data)
	}
	write(t, tree.Root, ".standards/tasks.project.toml", extension+"depends = [\"cycle\"]\n[cycle]\ndepends = [\"project:bootstrap\"]\nrun = \"echo unreachable\"\n")
	if output, err := run(""); err == nil || !strings.Contains(strings.ToLower(output), "circular") {
		t.Fatalf("dependency cycle was not rejected: %v\n%s", err, output)
	}
}
