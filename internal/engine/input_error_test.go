package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bundle "github.com/tqer39/dev-standards"
)

// 合成した値。本物の認証情報ではない。
const (
	leakURL   = "https://user:" + marker + "@example.com/simple?token=" + marker
	leakNames = "example.com"
)

// 英数字だけの値。URL や記号がなくても、秘密でないとは言えない。
var plainMarker = strings.ToLower(marker)

func assertNoLeak(t *testing.T, what, text string) {
	t.Helper()
	if strings.Contains(strings.ToLower(text), plainMarker) || strings.Contains(text, leakNames) {
		t.Fatalf("%s quotes its input: %s", what, text)
	}
}

// Plan、Check、Verify が読む入力の解析エラーは、入力の値を転載せず、ファイルと行だけを示す。
func TestInputErrorsNeverQuoteInputs(t *testing.T) {
	goAdopted := func(t *testing.T) (*Tree, Request) {
		tree := fixture(t)
		adopt(t, tree)
		return tree, Request{Mode: "reapply"}
	}
	projectAdopted := func(t *testing.T) (*Tree, Request) {
		tree, req := projectFixture(t)
		plan, err := tree.Plan(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := tree.Apply(plan); err != nil {
			t.Fatal(err)
		}
		return tree, Request{Mode: "reapply"}
	}
	companions := func(content string) func(*testing.T) (*Tree, Request) {
		return func(t *testing.T) (*Tree, Request) {
			tree, req := nodeFixture(t)
			write(t, tree.Root, "harness/go.mod", "module example.com/harness\n")
			write(t, tree.Root, "companions.json", content)
			req.CompanionsPath = "companions.json"
			return tree, req
		}
	}
	fresh := func(t *testing.T) (*Tree, Request) {
		return fixture(t), Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}
	}
	brokenTOML := "ok = 1\nvalue = \"" + leakURL + "\nnext = [" + marker
	brokenJSON := "{\n\"" + marker + "\": \"" + leakURL + "\",\n\"x\": " + marker + "}"
	cases := map[string]struct {
		fixture func(*testing.T) (*Tree, Request)
		name    string
		content string
		// want はエラーに残す、値を含まない位置の情報。
		want string
	}{
		"uv.lock syntax":            {pythonFixture, "session-indexer/uv.lock", brokenTOML, "session-indexer/uv.lock cannot be parsed (line 2)"},
		"uv.lock duplicate key":     {pythonFixture, "session-indexer/uv.lock", "version = 1\n\"" + leakURL + "\" = 1\n\"" + leakURL + "\" = 2\n", "session-indexer/uv.lock cannot be parsed (line 3)"},
		"pyproject.toml syntax":     {pythonFixture, "session-indexer/pyproject.toml", brokenTOML, "session-indexer/pyproject.toml cannot be parsed (line 2)"},
		"pyproject.toml wrong type": {pythonFixture, "session-indexer/pyproject.toml", "[project]\nname = [\"" + leakURL + "\"]\n", "session-indexer/pyproject.toml cannot be parsed"},
		"package.json syntax":       {nodeFixture, "package.json", brokenJSON, "package.json cannot be parsed (line 3)"},
		"package.json wrong type":   {nodeFixture, "package.json", "{\n\"scripts\": \"" + leakURL + "\"}", "package.json cannot be parsed (line 2)"},
		"package.json raw newline":  {nodeFixture, "package.json", "{\"name\": \"" + leakURL + "\n\"}", "package.json cannot be parsed (line 1)"},
		"package-lock.json syntax":  {nodeFixture, "package-lock.json", brokenJSON, "package-lock.json cannot be parsed (line 3)"},
		"package-lock.json version": {nodeFixture, "package-lock.json", "{\"lockfileVersion\": \"" + leakURL + "\"}", "package-lock.json cannot be parsed (line 1)"},
		"mise.toml syntax":          {fresh, "mise.toml", brokenTOML, "mise project configuration cannot be parsed (line 2)"},
		"mise tool key":             {projectFixture, "mise.toml", "[tools]\n\"" + leakURL + "\" = \"latest\"\n", "every tool in the tools table must be pinned explicitly"},
		"mise plain tool key":       {projectAdopted, ".standards/mise.project.toml", "[tools]\n" + plainMarker + " = \"latest\"\n", "every tool in the tools table must be pinned explicitly"},
		"profile value":             {goAdopted, ".standards/repository.yml", strings.Replace(string(JSON(defaultManifest())), `"profile": "go"`, `"profile": "`+plainMarker+`"`, 1), "unsupported profile; supported profiles"},
		"companion task name":       {companions(`[{"path":"harness","profile":"go","verifyTask":"project:verify:` + plainMarker + `"}]`), "harness/main.go", "package main\n", "define the verification task of companion 1"},
		"companion duplicate path":  {companions(`[{"path":"harness","profile":"go","verifyTask":"project:verify:a"},{"path":"harness","profile":"go","verifyTask":"project:verify:` + plainMarker + `"}]`), "harness/main.go", "package main\n", "duplicate component path in companion 2"},
		"package.json ends early":   {nodeFixture, "package.json", "{\"name\":\n", "package.json cannot be parsed (line 2)"},
		"package.json ends on line": {nodeFixture, "package.json", "{\"name\":", "package.json cannot be parsed (line 1)"},
		"editorconfig line":         {fresh, ".editorconfig", "[*]\nindent_size = 2\nhttps://user:" + marker + "@example.com/simple\n", "invalid EditorConfig line 3"},
		"hook source":               {func(t *testing.T) (*Tree, Request) { tree, req := fresh(t); req.Automation = true; return tree, req }, ".codex/hooks.json", brokenJSON, "hook source .standards/hooks.local.json cannot be parsed (line 3)"},
		"repository.yml syntax":     {goAdopted, ".standards/repository.yml", "schemaVersion: 1\n" + marker + ": [" + leakURL, "repository.yml: invalid YAML at line"},
		"repository.yml duplicate":  {goAdopted, ".standards/repository.yml", "{\"" + marker + "\": 1,\n\"" + marker + "\": \"" + leakURL + "\"}", "repository.yml: mapping keys must be unique strings (line 2)"},
		"repository.yml alias":      {goAdopted, ".standards/repository.yml", "a: &" + marker + " 1\nb: *" + marker + "\n", "repository.yml: anchors, aliases and explicit tags are not allowed (line 1)"},
		"repository.yml unknown":    {goAdopted, ".standards/repository.yml", "{\"" + marker + "\": \"" + leakURL + "\"}", "repository.yml: document does not match its schema at the top level"},
		"exceptions.yml syntax":     {goAdopted, ".standards/exceptions.yml", "schemaVersion: 1\nexceptions: [" + leakURL, "exceptions.yml: invalid YAML at line"},
		"lock.json unknown field":   {goAdopted, ".standards/lock.json", "{\"" + marker + "\": \"" + leakURL + "\"}", ".standards/lock.json cannot be parsed"},
		"lock.json syntax":          {goAdopted, ".standards/lock.json", brokenJSON, ".standards/lock.json cannot be parsed"},
		"state.json unknown field":  {goAdopted, ".standards/state.json", "{\"" + marker + "\": \"" + leakURL + "\"}", ".standards/state.json cannot be parsed"},
		"tasks.project.toml syntax": {goAdopted, ".standards/tasks.project.toml", brokenTOML, ".standards/tasks.project.toml cannot be parsed (line 2)"},
		"mise.project.toml syntax":  {goAdopted, ".standards/mise.project.toml", brokenTOML, "mise project configuration cannot be parsed (line 2)"},
		"editorconfig.local line":   {goAdopted, ".standards/editorconfig.local", "[*]\nhttps://user:" + marker + "@example.com/simple\n", "invalid EditorConfig line 2"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			tree, req := c.fixture(t)
			write(t, tree.Root, c.name, c.content)
			_, err := tree.Plan(req)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("the plan accepted a broken %s: %v", c.name, err)
			}
			assertNoLeak(t, "the plan error", err.Error())
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the error lost where it failed: got %q, want %q", err, c.want)
			}
			if req.Mode != "reapply" {
				return
			}
			// check と verify は Plan のエラーをそのまま返すので、報告も含めて確かめる。
			for name, run := range map[string]func() (Report, error){"check": tree.Check, "verify": func() (Report, error) { return tree.Verify(nil) }} {
				report, err := run()
				if ExitCode(err) != 2 || len(report.Results) != 0 {
					t.Fatalf("%s judged a broken %s: %+v %v", name, c.name, report, err)
				}
				assertNoLeak(t, name, string(JSON(report))+err.Error())
			}
		})
	}
}

// 採用済みの状態を読む経路（state のキー、保存した計画、lock の識別）も、値を転載しない。
func TestStoredStateErrorsNeverQuoteInputs(t *testing.T) {
	tree := fixture(t)
	plan := adopt(t, tree)

	state := read(t, tree.Root, ".standards/state.json")
	write(t, tree.Root, ".standards/state.json", strings.Replace(state, `"files": {`, `"files": {"`+leakURL+`": {"exists": true, "content": "", "mode": 420},`, 1))
	_, err := tree.Plan(Request{Mode: "reapply"})
	if err == nil || err.Error() != "unknown managed path in previous state" {
		t.Fatalf("an unknown state key was accepted or lost its message: %v", err)
	}
	assertNoLeak(t, "the state error", err.Error())
	write(t, tree.Root, ".standards/state.json", state)

	saved := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(saved, []byte(strings.Replace(string(JSON(plan)), `"schemaVersion": 1,`, `"schemaVersion": 1, "`+marker+`": "`+leakURL+`",`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadPlan(saved); err == nil || !strings.Contains(err.Error(), "plan cannot be parsed") {
		t.Fatalf("an edited plan was accepted or lost its message: %v", err)
	}
	assertNoLeak(t, "the saved plan error", err.Error())

	write(t, tree.Root, ".standards/lock.json", "{\n\"version\": "+marker+"}")
	if _, err = tree.AppliedStandard(); err == nil || !strings.Contains(err.Error(), ".standards/lock.json cannot be parsed (line 2)") {
		t.Fatalf("a broken lock was accepted or lost its message: %v", err)
	}
	assertNoLeak(t, "the applied standard error", err.Error())
	if _, err = tree.Inspect(); err == nil {
		t.Fatal("inspect accepted a broken lock")
	}
	assertNoLeak(t, "the inspection error", err.Error())
}

// スキーマの不一致は、スキーマ側のキーワードと、素直な形のキーだけで位置を示す。
func TestSchemaErrorsShowOnlyPlainLocations(t *testing.T) {
	var manifest map[string]any
	if err := bundle.Decode(JSON(defaultManifest()), "repository", &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["components"].([]any)[0].(map[string]any)["name"] = leakURL
	err := bundle.Decode(JSON(manifest), "repository", nil)
	if err == nil || err.Error() != "document does not match its schema at /components/0/name (pattern)" {
		t.Fatalf("unexpected schema error: %v", err)
	}
	// キーが自由な場所では、キーそのものが入力の値になる。
	err = bundle.Decode([]byte(`[{"path": "a", "profile": "go", "verifyTask": {"`+leakURL+`": 1}}]`), "companions", nil)
	if err == nil {
		t.Fatal("a malformed companion was accepted")
	}
	assertNoLeak(t, "the schema error", err.Error())
}

func TestFormatCheckErrorNeverQuotesTheSource(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, "main.go", "package main\n\nfunc main() { "+marker+" := \""+leakURL+"\" + }\n")
	_, err := tree.FormatCheck()
	if err == nil || err.Error() != "main.go cannot be parsed (line 3)" {
		t.Fatalf("unexpected format error: %v", err)
	}
}

// Plan が受け付けない入力は、判定の前提を欠く設定不正として終了コード 2 で止まる。
// 同じ lock でも、Plan が中身を検証しない mise-project では check が判定して終了コード 1 になる。
func TestRejectedPlanInputStopsBeforeCheckResults(t *testing.T) {
	const oldLock = `{"lockfileVersion":1,"dependencies":{"a":{"version":"1.0.0","resolved":"` + leakURL + `"}}}`
	tree, req := nodeFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	write(t, tree.Root, "package-lock.json", oldLock)
	const rejection = "package-lock.json must use lockfileVersion 2 or later"
	if _, err := tree.Plan(Request{Mode: "reapply"}); ExitCode(err) != 2 || err.Error() != rejection {
		t.Fatalf("plan: %v", err)
	}
	if _, err := tree.Plan(Request{Mode: "upgrade"}); ExitCode(err) != 2 || err.Error() != rejection {
		t.Fatalf("upgrade plan: %v", err)
	}
	report, err := tree.Check()
	if ExitCode(err) != 2 || err.Error() != rejection || len(report.Results) != 0 {
		t.Fatalf("check judged an input the plan rejects: %+v %v", report, err)
	}
	if report, err = tree.Verify(nil); ExitCode(err) != 2 || report.Scope != "configuration-only" {
		t.Fatalf("verify ran after the plan rejected its input: %+v %v", report, err)
	}

	project, req := projectFixture(t)
	write(t, project.Root, "package.json", `{"name":"root"}`)
	write(t, project.Root, "package-lock.json", `{"name":"root","lockfileVersion":3,"packages":{}}`)
	if plan, err = project.Plan(req); err != nil {
		t.Fatal(err)
	}
	if err := project.Apply(plan); err != nil {
		t.Fatal(err)
	}
	write(t, project.Root, "package-lock.json", oldLock)
	report, err = project.Check()
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), lockfileSourceRule) {
		t.Fatalf("check accepted an old lock: %v", err)
	}
	if source := dependencyStatuses(t, report)["source"]; source.Status != "fail" || !strings.Contains(source.Evidence, "package-lock.json: cannot be parsed") {
		t.Fatalf("unexpected source result: %+v", source)
	}
	assertNoLeak(t, "the report", string(JSON(report))+err.Error())
}

// Check が読んでいる間の変更も、検証記録を無効にする。
func TestVerifyDetectsChangesMadeWhileCheckRuns(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	commit(t, tree.Root)
	passing := func(string) error { return nil }
	const receipt = ".standards/cache/verified.json"

	// Check が判定を終えて戻るまでの間に、判定済みのファイルが変わる。
	report, err := tree.verify(func() (Report, error) {
		report, err := tree.Check()
		write(t, tree.Root, "main.go", "package main\n\nfunc main() { println() }\n")
		return report, err
	}, passing)
	if err == nil || err.Error() != "files changed during verification" {
		t.Fatalf("a change made during the check was verified: %v", err)
	}
	if report.Scope != "configuration-and-project-tasks" {
		t.Fatalf("the change was not detected at the end of the tasks: %s", report.Scope)
	}
	if _, err := os.Stat(filepath.Join(tree.Root, receipt)); !os.IsNotExist(err) {
		t.Fatalf("a receipt was written for a changed tree: %v", err)
	}
	if tree.ReceiptValid() {
		t.Fatal("a changed tree has a valid receipt")
	}

	// 変更がなければ、同じ手順で検証記録を残す。
	if _, err := tree.verify(tree.Check, passing); err != nil {
		t.Fatal(err)
	}
	if !tree.ReceiptValid() {
		t.Fatal("an unchanged tree has no valid receipt")
	}

	// タスクの途中の変更は、これまでどおり検出する。
	changed := false
	_, err = tree.verify(tree.Check, func(string) error {
		if !changed {
			changed = true
			write(t, tree.Root, "main.go", "package main\n\nfunc main() {}\n")
		}
		return nil
	})
	if err == nil || err.Error() != "files changed during verification" || tree.ReceiptValid() {
		t.Fatalf("a change made during the tasks was verified: %v", err)
	}
}
