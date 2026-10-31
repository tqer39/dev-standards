package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tqer39/dev-standards/internal/automation"
	"github.com/tqer39/dev-standards/internal/engine"
)

const languageRecord = "# 言語の判断\n\n## 要件\n\n複数の OS で小さなプログラムを実行する。\n\n## 候補\n\nGo と Python を配布と保守の面で比較する。\n\n## 選定理由\n\nGo を使い、単体の実行ファイルを配布する。\n"

func TestCLIHelpAndErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{{nil, 0}, {[]string{"version"}, 0}, {[]string{"catalog"}, 0}, {[]string{"adopt", "--help"}, 0}, {[]string{"unknown"}, 2}, {[]string{"adopt", "--unknown"}, 2}} {
		var out, err bytes.Buffer
		if code := run(tc.args, &out, &err); code != tc.code {
			t.Fatalf("%v: got %d expected %d: %s", tc.args, code, tc.code, &err)
		}
	}
}

func TestExecutableLifecycleWithMise(t *testing.T) {
	if _, err := exec.LookPath("mise"); err != nil {
		t.Fatal("mise is required for the acceptance test")
	}
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sandbox := t.TempDir()
	bin := filepath.Join(sandbox, "bin")
	if err = os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	name := "dev-standards"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(bin, name)
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/dev-standards")
	cmd.Dir = source
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %s: %v", b, err)
	}
	target := filepath.Join(sandbox, "project")
	if err = os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{"go.mod": "module example.com/fixture\n\ngo 1.27.3\n", "main.go": "package main\n\nfunc main() {}\n", "decision.md": languageRecord} {
		if err = os.WriteFile(filepath.Join(target, p), []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if b, err := exec.Command("git", "-C", target, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git: %s %v", b, err)
	}
	env := []string{}
	for _, v := range os.Environ() {
		upper := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		if upper == "PATH" || strings.HasPrefix(upper, "MISE_") {
			continue
		}
		env = append(env, v)
	}
	env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "MISE_CONFIG_DIR="+filepath.Join(sandbox, "mise-config"), "MISE_TRUSTED_CONFIG_PATHS="+target, "MISE_YES=1")
	runProcess := func(exe string, args []string, want int) []byte {
		t.Helper()
		c := exec.Command(exe, args...)
		c.Dir = target
		c.Env = env
		var out, log bytes.Buffer
		c.Stdout = &out
		c.Stderr = &log
		err := c.Run()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != want {
			t.Fatalf("%s %v: exit %d want %d\n%s\n%s", exe, args, code, want, &out, &log)
		}
		return out.Bytes()
	}
	initial := filepath.Join(sandbox, "initial.json")
	audit := runProcess(binary, []string{"audit"}, 1)
	if !bytes.Contains(audit, []byte(".editorconfig")) {
		t.Fatal("missing EditorConfig was not discovered")
	}
	runProcess(binary, []string{"onboard", "--decision", "decision.md"}, 2)
	if _, err := os.Stat(filepath.Join(target, ".standards/repository.yml")); !os.IsNotExist(err) {
		t.Fatal("onboarding mutated before fleet validation")
	}
	runProcess(binary, []string{"inspect"}, 0)
	runProcess(binary, []string{"adopt", "--profile", "go", "--decision", "decision.md", "--output", initial}, 1)
	runProcess(binary, []string{"apply", "--plan", initial}, 0)
	// Isolated global config: no user packages, services or shell setup run.
	runProcess("mise", []string{"bootstrap", "--only", "tools,task", "--yes"}, 0)
	before, err := os.ReadFile(filepath.Join(target, ".standards/state.json"))
	if err != nil {
		t.Fatal(err)
	}
	runProcess("mise", []string{"bootstrap", "--only", "tools,task", "--yes"}, 0)
	after, err := os.ReadFile(filepath.Join(target, ".standards/state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("second bootstrap changed standards state")
	}
	runProcess(binary, []string{"check"}, 0)
	runProcess(binary, []string{"verify"}, 0)
	b := runProcess(binary, []string{"plan"}, 0)
	var p engine.Plan
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 0 {
		t.Fatal("bootstrap or verification broke idempotency")
	}
	again := filepath.Join(sandbox, "again.json")
	runProcess(binary, []string{"plan", "--output", again}, 0)
	runProcess(binary, []string{"apply", "--plan", again}, 0)
	fleet := filepath.Join(sandbox, "fleet.json")
	config := automation.Default(automation.Entry{Path: source, Remote: filepath.Join(sandbox, "source.git"), Branch: "main", Role: "source"})
	if err := automation.Save(fleet, config); err != nil {
		t.Fatal(err)
	}
	runProcess("git", []string{"remote", "add", "origin", filepath.Join(sandbox, "project.git")}, 0)
	runProcess(binary, []string{"onboard", "--fleet", fleet}, 0)
	runProcess(binary, []string{"sync"}, 0)
	runProcess(binary, []string{"audit"}, 0)
	runProcess(binary, []string{"verify"}, 0)
	if b := runProcess(binary, []string{"sync"}, 0); !bytes.Contains(b, []byte("applied 0 changes")) {
		t.Fatal("sync is not idempotent")
	}
	registered, err := automation.Load(fleet)
	if err != nil || len(registered.Projects) != 1 || !registered.Projects[0].Canary {
		t.Fatal("initial enrollment was not saved")
	}

}

func TestPythonCLIPlanAndOnboard(t *testing.T) {
	target := t.TempDir()
	if b, err := exec.Command("git", "-C", target, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("%s %v", b, err)
	}
	for name, content := range map[string]string{
		"pyproject.toml": "[project]\nname='sample'\n[build-system]\nbuild-backend='hatchling.build'\n",
		"uv.lock":        "version=1\n",
		"mise.toml":      "[tools]\npython='3.13.0'\nuv='0.11.32'\n",
		"decision.md":    languageRecord,
	} {
		if err := os.WriteFile(filepath.Join(target, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(want int, args ...string) []byte {
		t.Helper()
		var out, log bytes.Buffer
		args = append(args, "--repo", target)
		if got := run(args, &out, &log); got != want {
			t.Fatalf("%v: got %d want %d: %s", args, got, want, log.String())
		}
		return out.Bytes()
	}
	inspection := invoke(0, "inspect")
	if !bytes.Contains(inspection, []byte(`"python"`)) {
		t.Fatal("Python not discovered")
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	invoke(1, "adopt", "--profile", "python-uv", "--component-path", ".", "--decision", "decision.md", "--output", planPath)
	invoke(0, "apply", "--plan", planPath)
	invoke(0, "plan")
	// Onboard selects the same Python profile for a fresh package.
	fresh := t.TempDir()
	for _, name := range []string{"pyproject.toml", "uv.lock", "decision.md"} {
		b, _ := os.ReadFile(filepath.Join(target, name))
		if err := os.WriteFile(filepath.Join(fresh, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fresh, "mise.toml"), []byte("[tools]\npython='3.13.0'\nuv='0.11.32'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"remote", "add", "origin", "https://github.com/example/python"}} {
		if b, err := exec.Command("git", append([]string{"-C", fresh}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", b, err)
		}
	}
	fleet := filepath.Join(t.TempDir(), "fleet.json")
	source, _ := filepath.Abs("../..")
	if err := automation.Save(fleet, automation.Default(automation.Entry{Path: source, Remote: "https://github.com/example/source", Branch: "main", Role: "source"})); err != nil {
		t.Fatal(err)
	}
	target = fresh
	invoke(0, "onboard", "--profile", "python-uv", "--decision", "decision.md", "--fleet", fleet)
	if b := invoke(0, "sync"); !bytes.Contains(b, []byte("applied 0 changes")) {
		t.Fatal("Python onboard/sync is not idempotent")
	}
}
