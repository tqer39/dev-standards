package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	bundle "github.com/tqer39/dev-standards"
)

const decision = "# 開発言語の選定\n\n## 要件\n\n複数の OS で動作する小さな CLI を作る。\n\n## 候補\n\nGo と Python を比較し、配布方法を評価する。\n\n## 選定理由\n\nGo を選び、単体の実行ファイルとして配布する。\n"

func fixture(t *testing.T) *Tree {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "--quiet")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	write(t, dir, "go.mod", "module example.com/project\n\ngo 1.27.3\n")
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "decision.md", decision)
	tree, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tree.Close() })
	return tree
}
func write(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(content), 0644); e != nil {
		t.Fatal(e)
	}
}
func read(t *testing.T, root, name string) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(root, name))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func adopt(t *testing.T, tree *Tree) *Plan {
	t.Helper()
	p, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
	if e != nil {
		t.Fatal(e)
	}
	if e = tree.Apply(p); e != nil {
		t.Fatal(e)
	}
	return p
}
func commit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "fixture"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%s: %v", b, e)
		}
	}
}

func TestInspectFindsPythonWithoutManifest(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, ".venv/lib/ignored.py", "")
	inspection, err := tree.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if contains(inspection.Languages, "python") {
		t.Fatal("Python in a hidden environment must not identify the project")
	}
	write(t, tree.Root, "catalog/main.py", "print('hello')\n")
	inspection, err = tree.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(inspection.Languages, "python") {
		t.Fatal("Python source without pyproject.toml must be detected")
	}
}

func TestAdoptReapplyOffline(t *testing.T) {
	tree := fixture(t)
	p, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Changes) < 10 {
		t.Fatalf("too few managed inputs: %d", len(p.Changes))
	}
	if _, e := os.Stat(filepath.Join(tree.Root, ".standards")); !os.IsNotExist(e) {
		t.Fatal("planning changed target")
	}
	if e := tree.Apply(p); e != nil {
		t.Fatal(e)
	}
	commit(t, tree.Root)
	again, e := tree.Plan(Request{Mode: "reapply"})
	if e != nil {
		t.Fatal(e)
	}
	if len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("not idempotent: %+v", again)
	}
	if e = tree.Apply(again); e != nil {
		t.Fatal(e)
	}
	report, e := tree.Check()
	if e != nil {
		t.Fatal(e)
	}
	if report.Scope != "configuration-only" {
		t.Fatal("overstated verification")
	}
	found := false
	for _, r := range report.Results {
		if r.Status == "not_run" {
			found = true
		}
	}
	if !found {
		t.Fatal("unexecuted checks reported as complete")
	}
}
func TestPreservesExistingConfiguration(t *testing.T) {
	tree := fixture(t)
	oldIgnore := "# custom ignore\nmy-output/\n"
	oldMise := "# Keep this comment\n[tools]\ngo = \"1.27.3\"\n\n[tasks.dev]\nrun = \"go run .\"\n"
	oldEditor := "root = true\n# Go settings\n[*.go]\nindent_style = tab\nindent_size = 4\n"
	write(t, tree.Root, ".gitignore", oldIgnore)
	write(t, tree.Root, "mise.toml", oldMise)
	write(t, tree.Root, ".editorconfig", oldEditor)
	write(t, tree.Root, "unrelated.txt", "user data")
	adopt(t, tree)
	if read(t, tree.Root, ".standards/gitignore.local") != oldIgnore {
		t.Fatal("gitignore lost")
	}
	if read(t, tree.Root, ".standards/mise.project.toml") != oldMise {
		t.Fatal("mise source lost")
	}
	if !strings.Contains(read(t, tree.Root, "mise.toml"), oldMise) {
		t.Fatal("mise comment or task lost")
	}
	if !strings.Contains(read(t, tree.Root, ".editorconfig"), "# Go settings") {
		t.Fatal("editor comment lost")
	}
	if read(t, tree.Root, "unrelated.txt") != "user data" {
		t.Fatal("unrelated change")
	}
	p, e := tree.Plan(Request{Mode: "reapply"})
	if e != nil || len(p.Changes) != 0 {
		t.Fatalf("reapply: %+v %v", p, e)
	}
}
func TestSourceEditsRegenerate(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	write(t, tree.Root, ".standards/gitignore.local", "custom-generated/\n")
	p, e := tree.Plan(Request{Mode: "reapply"})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Conflicts) != 0 || len(p.Changes) == 0 {
		t.Fatal("source edit did not produce a clean plan")
	}
	if e = tree.Apply(p); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(read(t, tree.Root, ".gitignore"), "custom-generated/") {
		t.Fatal("source not applied")
	}
	p, e = tree.Plan(Request{Mode: "reapply"})
	if e != nil || len(p.Changes) != 0 {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestManagedFileConflict(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	write(t, tree.Root, ".editorconfig", "user changed this\n")
	p, e := tree.Plan(Request{Mode: "reapply"})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Conflicts) == 0 {
		t.Fatal("missed drift")
	}
	if e = tree.Apply(p); e == nil {
		t.Fatal("overwrote conflict")
	}
	if read(t, tree.Root, ".editorconfig") != "user changed this\n" {
		t.Fatal("lost user's edit")
	}
	if _, e = tree.Check(); ExitCode(e) != 1 {
		t.Fatalf("check: %v", e)
	}
}
func TestStaleAndTamperedPlans(t *testing.T) {
	for _, kind := range []string{"input", "head", "path", "content", "other-root"} {
		t.Run(kind, func(t *testing.T) {
			tree := fixture(t)
			p, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "input":
				write(t, tree.Root, "go.mod", "module changed.invalid\n")
			case "head":
				commit(t, tree.Root)
			case "path":
				p.Changes[0].Path = "../outside"
			case "content":
				p.Changes[0].After.Content = "modified malicious content"
			case "other-root":
				p.Root = t.TempDir()
			}
			if e = tree.Apply(p); e == nil {
				t.Fatal("unsafe plan applied")
			}
			if _, e = os.Stat(filepath.Join(tree.Root, ".standards")); !os.IsNotExist(e) {
				t.Fatal("partially applied stale or modified plan")
			}
		})
	}
}
func TestRollback(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, ".gitignore", "custom/\n")
	p, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
	if e != nil {
		t.Fatal(e)
	}
	e = tree.apply(p, func(i int) error {
		if i == 5 {
			return errors.New("simulated write failure")
		}
		return nil
	})
	if e == nil {
		t.Fatal("missing simulated failure")
	}
	for name, want := range p.Inputs {
		got, e := tree.Read(name)
		if e != nil || !equal(got, want) {
			t.Fatalf("rollback %s: %+v != %+v: %v", name, got, want, e)
		}
	}
	if _, err := os.Stat(filepath.Join(tree.Root, ".standards")); !os.IsNotExist(err) {
		t.Fatal("rollback left newly created directories")
	}
	if read(t, tree.Root, ".gitignore") != "custom/\n" {
		t.Fatal("original ignore lost")
	}
}
func TestSymlinksAndUnsafePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs additional Windows privileges")
	}
	for _, name := range []string{".standards", ".editorconfig"} {
		t.Run(name, func(t *testing.T) {
			tree := fixture(t)
			outside := t.TempDir()
			if name == ".editorconfig" {
				write(t, outside, "target", "private")
				outside = filepath.Join(outside, "target")
			}
			if e := os.Symlink(outside, filepath.Join(tree.Root, name)); e != nil {
				t.Fatal(e)
			}
			if _, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}); e == nil {
				t.Fatal("symlink accepted")
			}
		})
	}
	for _, p := range []string{"../out", ".git/config", "a/../../out", "C:\\file", "/absolute", "a/../b", "a//b"} {
		if e := validPath(p); e == nil {
			t.Errorf("accepted %q", p)
		}
	}
}
func TestLockAndStateCannotBypassExpectedContent(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	var s State
	if e := strictJSON(read(t, tree.Root, ".standards/state.json"), &s); e != nil {
		t.Fatal(e)
	}
	write(t, tree.Root, ".editorconfig", "fake\n")
	s.Files[".editorconfig"] = file("fake\n")
	write(t, tree.Root, ".standards/state.json", string(JSON(s)))
	if _, e := tree.Check(); ExitCode(e) != 1 {
		t.Fatalf("tampering bypassed check: %v", e)
	}
	var l Lock
	if e := strictJSON(read(t, tree.Root, ".standards/lock.json"), &l); e != nil {
		t.Fatal(e)
	}
	l.Bundle = "fake"
	write(t, tree.Root, ".standards/lock.json", string(JSON(l)))
	if _, e := tree.Plan(Request{Mode: "reapply"}); e == nil {
		t.Fatal("accepted altered lock")
	}
}
func TestInvalidAdoptionInputs(t *testing.T) {
	cases := map[string]func(*Tree){
		"reserved-task":    func(tr *Tree) { write(t, tr.Root, "mise.toml", "[tasks.test]\nrun = \"echo custom\"\n") },
		"task-includes":    func(tr *Tree) { write(t, tr.Root, "mise.toml", "[task_config]\nincludes = [\"custom\"]\n") },
		"task-directory":   func(tr *Tree) { write(t, tr.Root, "mise-tasks/test", "echo custom") },
		"untracked-rules":  func(tr *Tree) { write(t, tr.Root, ".gitignore", ".standards/\n") },
		"private-override": func(tr *Tree) { write(t, tr.Root, ".gitignore", "!.env\n") },
		"wrong-editor":     func(tr *Tree) { write(t, tr.Root, ".editorconfig", "[*]\nend_of_line=crlf\n") },
		"no-decision":      func(tr *Tree) { write(t, tr.Root, "decision.md", "# Go selected\n") },
		"floating-tool":    func(tr *Tree) { write(t, tr.Root, "mise.toml", "[tools]\ngo=\"latest\"\n") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			tree := fixture(t)
			mutate(tree)
			if _, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}); e == nil {
				t.Fatal("accepted unsupported/unsafe input")
			}
		})
	}
}
func TestUnrelatedChangesAfterPlanSurvive(t *testing.T) {
	tree := fixture(t)
	p, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
	if e != nil {
		t.Fatal(e)
	}
	write(t, tree.Root, "unrelated.txt", "keep")
	if e = tree.Apply(p); e != nil {
		t.Fatal(e)
	}
	if read(t, tree.Root, "unrelated.txt") != "keep" {
		t.Fatal("lost change")
	}
}
func TestApplyLock(t *testing.T) {
	tree := fixture(t)
	p, e := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"})
	if e != nil {
		t.Fatal(e)
	}
	write(t, tree.Root, ".dev-standards-apply.lock", "another operation")
	if e = tree.Apply(p); e == nil {
		t.Fatal("concurrent apply accepted")
	}
}
func TestCommittedDeletionCanBeRestored(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	commit(t, tree.Root)
	if e := os.Remove(filepath.Join(tree.Root, ".editorconfig")); e != nil {
		t.Fatal(e)
	}
	p, e := tree.Plan(Request{Mode: "reapply"})
	if e != nil || len(p.Conflicts) == 0 {
		t.Fatal("uncommitted deletion not protected")
	}
	commit(t, tree.Root)
	p, e = tree.Plan(Request{Mode: "reapply"})
	if e != nil || len(p.Conflicts) != 0 {
		t.Fatalf("committed deletion: %+v %v", p, e)
	}
	if e = tree.Apply(p); e != nil {
		t.Fatal(e)
	}
}
func TestFormatCheck(t *testing.T) {
	tree := fixture(t)
	paths, e := tree.FormatCheck()
	if e != nil || len(paths) != 0 {
		t.Fatalf("%v %v", paths, e)
	}
	write(t, tree.Root, "main.go", "package main\nfunc main(){fmt.Println(1)}\n")
	paths, e = tree.FormatCheck()
	if e != nil || len(paths) != 1 {
		t.Fatalf("%v %v", paths, e)
	}
}
func TestUnsupportedConfiguration(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	var m Manifest
	if e := bundle.Decode([]byte(read(t, tree.Root, ".standards/repository.yml")), "repository", &m); e != nil {
		t.Fatal(e)
	}
	m.Components[0].Profile = "swift"
	write(t, tree.Root, ".standards/repository.yml", string(JSON(m)))
	if _, e := tree.Check(); e == nil {
		t.Fatal("unsupported profile accepted")
	}
}
func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses portable mode normalization")
	}
	tree := fixture(t)
	write(t, tree.Root, ".gitignore", "custom/\n")
	if e := os.Chmod(filepath.Join(tree.Root, ".gitignore"), 0600); e != nil {
		t.Fatal(e)
	}
	adopt(t, tree)
	for _, name := range []string{".standards/gitignore.local", ".standards/state.json"} {
		info, err := os.Stat(filepath.Join(tree.Root, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("import/state permissions not preserved: %s %v", name, err)
		}
	}
	p, e := tree.Plan(Request{Mode: "reapply"})
	if e != nil || len(p.Changes) != 0 || len(p.Conflicts) != 0 {
		t.Fatalf("mode not preserved: %+v %v", p, e)
	}
}
func ExampleExitCode() {
	fmt.Println(ExitCode(nil))
	fmt.Println(ExitCode(fail(1, "drift")))
	fmt.Println(ExitCode(errors.New("invalid")))
	// Output:
	// 0
	// 1
	// 2
}

func TestExistingAdoptionArtifactsAreNotOverwritten(t *testing.T) {
	for name, content := range map[string]string{decisionTarget: "", vendorPath: "different snapshot", ".standards/tasks.standards.toml": "user tasks"} {
		t.Run(name, func(t *testing.T) {
			tree := fixture(t)
			write(t, tree.Root, name, content)
			if _, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}); err == nil {
				t.Fatal("conflicting existing artifact accepted")
			}
			if read(t, tree.Root, name) != content {
				t.Fatal("existing artifact overwritten")
			}
		})
	}
}

func TestPreservesAttributesAndPortableInputs(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, ".gitattributes", "# custom attribute\n*.png binary\n")
	adopt(t, tree)
	if read(t, tree.Root, ".standards/gitattributes.local") != "# custom attribute\n*.png binary\n" {
		t.Fatal("attributes lost")
	}
	if !strings.Contains(read(t, tree.Root, ".gitattributes"), "/mise.toml text eol=lf") {
		t.Fatal("missing portable checkout policy")
	}
	commit(t, tree.Root)
	p, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(p.Changes) != 0 {
		t.Fatalf("committing changed adopted inputs: %+v %v", p, err)
	}
}

func TestCheckoutWithAutocrlfRemainsIdempotent(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, "mise.toml", "# preserved comment\r\n[tools]\r\ngo=\"1.27.3\"\r\n")
	write(t, tree.Root, ".gitignore", "# custom\r\nIcon\r\r\n")
	cmd := exec.Command("git", "-C", tree.Root, "config", "core.autocrlf", "true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	adopt(t, tree)
	commit(t, tree.Root)
	destination := filepath.Join(t.TempDir(), "checkout")
	cmd = exec.Command("git", "-c", "core.autocrlf=true", "clone", "--quiet", tree.Root, destination)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s %v", out, err)
	}
	clone, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	p, err := clone.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 0 || len(p.Conflicts) != 0 {
		t.Fatalf("checkout changed generation: changes=%v conflicts=%v", p.Changes, p.Conflicts)
	}
}
