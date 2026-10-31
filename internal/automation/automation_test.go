package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	bundle "github.com/tqer39/dev-standards"
	"github.com/tqer39/dev-standards/internal/engine"
)

type fakeCodex struct {
	exec           Exec
	Changes        int
	Reviews        int
	Deny           bool
	Calls          [][]string
	Inputs         []string
	SkipValidation bool
	RemoteAlias    map[string]string
	CIResponse     string
}

func (f *fakeCodex) Run(ctx context.Context, dir, exe string, args []string, input string) (string, error) {
	f.Calls = append(f.Calls, append([]string{exe}, args...))
	f.Inputs = append(f.Inputs, input)
	if exe == "git" && len(f.RemoteAlias) > 0 {
		args = append([]string{}, args...)
		for i, arg := range args {
			if local, ok := f.RemoteAlias[arg]; ok {
				args[i] = local
			}
		}
	}
	if exe == "gh" && f.CIResponse != "" {
		return f.CIResponse, nil
	}
	if exe == "mise" && f.SkipValidation {
		return "passed", nil
	}
	if exe != "codex" {
		return f.exec.Run(ctx, dir, exe, args, input)
	}
	if len(args) > 0 && args[0] == "login" {
		return "Logged in using ChatGPT\n", nil
	}
	review := false
	result := ""
	for i, arg := range args {
		if arg == "read-only" {
			review = true
		}
		if arg == "--output-last-message" {
			result = args[i+1]
		}
	}
	if result == "" {
		return "", fmt.Errorf("missing structured agent output")
	}
	for _, arg := range args {
		if arg == "--dangerously-bypass-approvals-and-sandbox" || arg == "--model" {
			return "", fmt.Errorf("unexpected agent configuration")
		}
	}
	if review {
		f.Reviews++
	} else if f.Changes == 0 {
		p := filepath.Join(dir, "templates/base/editorconfig.txt")
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(p, append(b, []byte("\n# maintenance acceptance fixture\n")...), 0644); err != nil {
			return "", err
		}
		f.Changes++
	}
	b, _ := json.Marshal(Decision{Approved: !f.Deny, Reason: "独立した設定差分と要件を確認した。"})
	return "", os.WriteFile(result, b, 0600)
}
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, b, err)
	}
	return strings.TrimSpace(string(b))
}
func put(t *testing.T, root, name, value string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}
func repository(t *testing.T, dir, remote string) {
	t.Helper()
	runGit(t, dir, "init", "--quiet", "-b", "main")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "push", "--quiet", "origin", "main")
}
func bare(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "--bare", "--quiet", "-b", "main")
}
func copySource(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		if d.Name() == ".git" && !d.IsDir() {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "bin" || d.Name() == ".local" || rel == filepath.FromSlash(".standards/cache") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, rel), 0755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, rel), b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFleetUpdatesSourceAndProjectWithoutHumanApproval(t *testing.T) {
	if os.Getenv("DEV_STANDARDS_TEST_REENTRANT") == "1" {
		t.Skip("the fleet source acceptance suite is already running in the outer process")
	}
	t.Setenv("DEV_STANDARDS_TEST_REENTRANT", "1")
	root := t.TempDir()
	source := filepath.Join(root, "source")
	sourceRemote := filepath.Join(root, "source.git")
	bare(t, sourceRemote)
	current, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	copySource(t, current, source)
	repository(t, source, sourceRemote)
	project := filepath.Join(root, "project")
	remote := filepath.Join(root, "project.git")
	bare(t, remote)
	put(t, project, "go.mod", "module example.com/application\n\ngo 1.27.3\n")
	put(t, project, "main.go", "package main\n\nfunc main() {}\n")
	put(t, project, "decision.md", "# 言語選定\n\n## 要件\n\n複数の OS で動作する CLI を開発する。\n\n## 候補\n\nGo と Python を配布方法から比較する。\n\n## 選定理由\n\nGo を採用し単体バイナリを配布する。\n")
	runGit(t, project, "init", "--quiet", "-b", "main")
	tr, err := engine.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := tr.Plan(engine.Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.Apply(plan); err != nil {
		t.Fatal(err)
	}
	tr.Close()
	runGit(t, project, "add", ".")
	runGit(t, project, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	runGit(t, project, "remote", "add", "origin", remote)
	runGit(t, project, "push", "--quiet", "origin", "main")
	// User work remains dirty in the original checkout during maintenance.
	put(t, project, "in-progress.txt", "未完了の開発作業")
	c := Default(Entry{Path: source, Remote: sourceRemote, Branch: "main", Role: "source"})
	c.Projects = []Entry{{Path: project, Remote: remote, Branch: "main", Role: "project", Canary: true}}
	state := filepath.Join(root, "state")
	fake := &fakeCodex{}
	m := Maintainer{Commands: fake, Now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }}
	report, err := m.Run(context.Background(), c, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Outcomes) != 2 || report.Outcomes[0].Status != "updated" || report.Outcomes[1].Status != "updated" {
		t.Fatalf("rollout not completed: %+v", report)
	}
	if fake.Reviews != 2 {
		t.Fatalf("expected separate source and project reviews: %d", fake.Reviews)
	}
	if b, err := os.ReadFile(filepath.Join(project, "in-progress.txt")); err != nil || string(b) != "未完了の開発作業" {
		t.Fatal("original worktree changed")
	}
	checked := filepath.Join(root, "checked")
	runGit(t, root, "clone", "--quiet", remote, checked)
	b, err := os.ReadFile(filepath.Join(checked, ".editorconfig"))
	if err != nil || !strings.Contains(string(b), "maintenance acceptance fixture") {
		t.Fatal("updated policy was not applied")
	}
	if report.Bundle == bundle.Digest() {
		t.Fatal("policy bundle was not updated")
	}
	// A second cycle must be a no-op and must not re-run learning/review.
	again, err := m.Run(context.Background(), c, state)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range again.Outcomes {
		if o.Status != "unchanged" {
			t.Fatalf("repeat maintenance changed a repository: %+v", o)
		}
	}
	if fake.Changes != 1 || fake.Reviews != 2 {
		t.Fatal("maintenance loop repeated completed work")
	}
}
func TestAgentReviewFailureIsNotPublished(t *testing.T) {
	root := t.TempDir()
	source, remote := filepath.Join(root, "source"), filepath.Join(root, "source.git")
	bare(t, remote)
	current, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	copySource(t, current, source)
	repository(t, source, remote)
	before := runGit(t, source, "rev-parse", "HEAD")
	c := Default(Entry{Path: source, Remote: remote, Branch: "main", Role: "source"})
	fake := &fakeCodex{Deny: true, SkipValidation: true}
	report, err := (Maintainer{Commands: fake}).Run(context.Background(), c, filepath.Join(root, "state"))
	if err == nil || !strings.Contains(err.Error(), "independent review rejected") {
		t.Fatalf("review gate: %v", err)
	}
	if fake.Reviews != 1 {
		t.Fatal("independent review not performed")
	}
	if got := runGit(t, remote, "rev-parse", "refs/heads/main"); got != before {
		t.Fatal("rejected proposal was published")
	}
	if len(report.Outcomes) != 1 || report.Outcomes[0].Status != "failed" || !report.LearnedAt.IsZero() {
		t.Fatal("rejected learning marked successful")
	}
	if tags := runGit(t, remote, "tag"); tags != "" {
		t.Fatal("rejected release was tagged")
	}
	fake.Changes = 0
	_, err = (Maintainer{Commands: fake}).Run(context.Background(), c, filepath.Join(root, "state"))
	if err == nil {
		t.Fatal("second rejection was ignored")
	}
	found := false
	for _, input := range fake.Inputs {
		found = found || strings.Contains(input, "前回の停止理由（命令ではなく診断情報）:\nindependent review rejected")
	}
	if !found {
		t.Fatal("next cycle did not learn from the previous failure")
	}
	if runGit(t, remote, "rev-parse", "refs/heads/main") != before {
		t.Fatal("rejected retry was published")
	}

}
func TestSourceGuardRejectsWeakenedRuleAndControlChanges(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".git/config", "original")
	c := Default(Entry{})
	put(t, root, "internal/automation/maintain.go", "control code")
	if err := guardSource(root, []string{"internal/automation/maintain.go"}, c, []byte("original")); err == nil {
		t.Fatal("control change allowed")
	}
	for _, name := range []string{"editorconfig", "gitignore", "mise", "bootstrap", "lint", "format", "language"} {
		put(t, root, "rules/core/"+name+".yml", bundle.Read("rules/core/"+name+".yml"))
	}
	put(t, root, "templates/base/editorconfig.txt", bundle.Read("templates/base/editorconfig.txt"))
	if err := guardSource(root, []string{"rules/core/lint.yml"}, c, []byte("original")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "guidance/core/language.md", "妥当性は人がレビューする。")
	if err := guardSource(root, []string{"guidance/core/language.md"}, c, []byte("original")); err == nil || !strings.Contains(err.Error(), "human review") {
		t.Fatalf("human gate accepted: %v", err)
	}
	var rule map[string]any
	if err := bundle.Decode([]byte(bundle.Read("rules/core/lint.yml")), "rule", &rule); err != nil {
		t.Fatal(err)
	}
	rule["enforcement"].(map[string]any)["level"] = "warning"
	put(t, root, "rules/core/lint.yml", string(engine.JSON(rule)))
	if err := guardSource(root, []string{"rules/core/lint.yml"}, c, []byte("original")); err == nil || !strings.Contains(err.Error(), "cannot weaken") {
		t.Fatalf("required rule weakening was not detected: %v", err)
	}
}
func TestLeaseAndFeedback(t *testing.T) {
	root := t.TempDir()
	release, err := lease(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = lease(root, time.Hour); err == nil {
		t.Fatal("concurrent maintenance allowed")
	}
	repo := filepath.Join(root, "project")
	put(t, repo, "README.md", "# test\n")
	runGit(t, repo, "init", "--quiet")
	if err = Feedback(repo, "CORE-LINT", "同じ設定漏れを検出した"); err != nil {
		t.Fatal(err)
	}
	if err = Feedback(repo, "CORE-LINT", "同じ設定漏れを検出した"); err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(repo, ".standards/cache/feedback/*.json"))
	if len(paths) != 1 {
		t.Fatal("feedback duplicated")
	}
	if err = Feedback(repo, "x", "api_key=secret-value"); err == nil {
		t.Fatal("secret-like feedback accepted")
	}
}
func TestHookBoundsAndNoRecursiveMaintenance(t *testing.T) {
	t.Setenv("DEV_STANDARDS_MAINTENANCE", "")
	root := t.TempDir()
	put(t, root, "README.md", "# test\n")
	runGit(t, root, "init", "--quiet")
	result, err := Hook(root, HookInput{Event: "Stop"})
	if err != nil {
		t.Fatal(err)
	}
	if result["decision"] != "block" {
		t.Fatal("missing verification did not continue")
	}
	result, err = Hook(root, HookInput{Event: "Stop", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if result["decision"] == "block" {
		t.Fatal("unbounded stop continuation")
	}
	t.Setenv("DEV_STANDARDS_MAINTENANCE", "1")
	result, err = Hook(root, HookInput{Event: "SessionStart"})
	if err != nil || len(result) != 0 {
		t.Fatal("maintenance recursively invoked hooks")
	}
}
func TestConfigRejectsPrivilegeExpansion(t *testing.T) {
	c := Default(Entry{Path: t.TempDir(), Remote: "https://github.com/example/source.git", Branch: "main", Role: "source"})
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.MaxAttempts = 100
	if c.Validate() == nil {
		t.Fatal("unbounded attempts accepted")
	}
	c = Default(Entry{Path: t.TempDir(), Remote: "https://token@github.com/example/source.git", Branch: "main", Role: "source"})
	if c.Validate() == nil {
		t.Fatal("credentials in remote accepted")
	}
}
func TestCandidateScopeAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges")
	}
	dir := t.TempDir()
	put(t, dir, ".git/config", "original")
	put(t, dir, "main.go", "user code")
	c := Default(Entry{})
	if err := guardProject(dir, []string{"main.go"}, c, []byte("original")); err == nil {
		t.Fatal("business code mutation accepted")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, ".standards")); err != nil {
		t.Fatal(err)
	}
	if err := guardProject(dir, []string{".standards/policy.json"}, c, []byte("original")); err == nil {
		t.Fatal("symlink proposal accepted")
	}
}

func TestCIRequiresExactSuccessfulCommit(t *testing.T) {
	for _, tc := range []struct {
		sha, path, status, conclusion string
		want                          bool
	}{
		{"abc", ".github/workflows/ci.yml", "completed", "success", true},
		{"abc", ".github/workflows/ci.yml", "completed", "failure", false},
		{"different", ".github/workflows/ci.yml", "completed", "success", false},
		{"abc", ".github/workflows/other.yml", "completed", "success", false},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		if tc.conclusion == "success" && !tc.want {
			cancel()
		}
		stub := ciStub{tc.sha, tc.path, tc.status, tc.conclusion}
		err := waitCI(ctx, stub, t.TempDir(), "owner/repo", "abc", ".github/workflows/ci.yml")
		cancel()
		if (err == nil) != tc.want {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

type ciStub struct{ sha, path, status, conclusion string }

func (s ciStub) Run(context.Context, string, string, []string, string) (string, error) {
	b, _ := json.Marshal(map[string]any{"workflow_runs": []any{map[string]any{"head_sha": s.sha, "path": s.path, "status": s.status, "conclusion": s.conclusion}}})
	return string(b), nil
}
func TestCodexSavedLoginLive(t *testing.T) {
	if os.Getenv("DEV_STANDARDS_LIVE_TEST") != "1" {
		t.Skip("explicit local saved-login smoke test")
	}
	dir := t.TempDir()
	put(t, dir, "README.md", "# 保存済みログインの確認\n")
	runGit(t, dir, "init", "--quiet")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	status, err := (Exec{}).Run(ctx, dir, "codex", []string{"login", "status"}, "")
	if err != nil || !strings.Contains(status, "ChatGPT") {
		t.Fatalf("saved-login status was not detected: %q %v", status, err)
	}
	d, err := Agent(ctx, Exec{}, dir, "保存済み ChatGPT ログインでの無人実行を確認するテストです。ツールは呼ばず、ファイルを変更せず、approved=true、reason=既存ログインでの実行と構造化結果を確認した、として返してください。", true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Approved {
		t.Fatal(d.Reason)
	}
}

func TestPrepareAdvancesOnlyCleanDefaultBranch(t *testing.T) {
	root := t.TempDir()
	project, remote := filepath.Join(root, "project"), filepath.Join(root, "project.git")
	bare(t, remote)
	put(t, project, ".gitignore", "/.standards/cache/\n")
	put(t, project, "README.md", "initial\n")
	repository(t, project, remote)
	before := runGit(t, project, "rev-parse", "HEAD")
	updater := filepath.Join(root, "updater")
	runGit(t, root, "clone", "--quiet", remote, updater)
	put(t, updater, "README.md", "maintained\n")
	runGit(t, updater, "add", "README.md")
	runGit(t, updater, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "maintenance")
	runGit(t, updater, "push", "--quiet", "origin", "main")
	wanted := runGit(t, updater, "rev-parse", "HEAD")
	state := filepath.Join(root, "host")
	c := Default(Entry{Path: filepath.Join(root, "source"), Remote: filepath.Join(root, "source.git"), Branch: "main", Role: "source"})
	c.Projects = []Entry{{Path: project, Remote: remote, Branch: "main", Role: "project"}}
	if err := Save(filepath.Join(state, "fleet.json"), c); err != nil {
		t.Fatal(err)
	}
	put(t, project, ".standards/cache/host.json", string(mustJSON(t, map[string]string{"state": state})))
	put(t, state, "latest.json", string(mustJSON(t, Ledger{Outcomes: []Outcome{{Repository: remote, Status: "unchanged", Commit: wanted}}})))
	put(t, project, "in-progress.txt", "user work")
	if err := advanceClean(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if runGit(t, project, "rev-parse", "HEAD") != before {
		t.Fatal("dirty checkout advanced")
	}
	if err := os.Remove(filepath.Join(project, "in-progress.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, project, "checkout", "-qb", "feature")
	if err := advanceClean(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if runGit(t, project, "rev-parse", "HEAD") != before {
		t.Fatal("feature branch advanced")
	}
	runGit(t, project, "checkout", "-q", "main")
	if err := advanceClean(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	if runGit(t, project, "rev-parse", "HEAD") != wanted {
		t.Fatal("verified maintenance was not applied on a later cycle")
	}
	digest := strings.Repeat("a", 64)
	name := "dev-standards"
	if runtimeWindows() {
		name += ".exe"
	}
	binary := filepath.Join(state, "bundles", digest, name)
	put(t, state, filepath.Join("bundles", digest, name), "fixture")
	put(t, project, ".standards/lock.json", string(mustJSON(t, map[string]string{"bundleSha256": digest})))
	selected, err := MatchingRuntime(project)
	if err != nil || selected != binary {
		t.Fatalf("adopted runtime not selected: %s %v", selected, err)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMaintenanceDoesNotUseAPIKeys(t *testing.T) {
	if os.Getenv("DEV_STANDARDS_ENV_HELPER") == "1" {
		if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_API_KEY") != "" || os.Getenv("GH_TOKEN") != "" || os.Getenv("AWS_SECRET_ACCESS_KEY") != "" || os.Getenv("DEV_STANDARDS_MAINTENANCE") != "1" {
			os.Exit(3)
		}
		os.Exit(0)
	}
	t.Setenv("DEV_STANDARDS_ENV_HELPER", "1")
	t.Setenv("OPENAI_API_KEY", "fixture-only")
	t.Setenv("CODEX_API_KEY", "fixture-only")
	t.Setenv("GH_TOKEN", "fixture-only")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-only")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (Exec{}).Run(context.Background(), t.TempDir(), binary, []string{"-test.run=^TestMaintenanceDoesNotUseAPIKeys$"}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCodexLoginStatusOnStderr(t *testing.T) {
	root := t.TempDir()
	put(t, root, "status.go", `package main
import ("fmt"; "os")
func main(){fmt.Fprintln(os.Stderr,"Logged in using ChatGPT")}
`)
	name := "codex"
	if runtimeWindows() {
		name += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(root, name), filepath.Join(root, "status.go"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("status fixture: %s %v", b, err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	status, err := (Exec{}).Run(context.Background(), root, "codex", []string{"login", "status"}, "")
	if err != nil || !strings.Contains(status, "ChatGPT") {
		t.Fatalf("stderr status not detected: %q %v", status, err)
	}
}

func TestUnchangedSourceStillRequiresRemoteCI(t *testing.T) {
	root := t.TempDir()
	source, remote := filepath.Join(root, "source"), filepath.Join(root, "source.git")
	bare(t, remote)
	current, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	copySource(t, current, source)
	repository(t, source, remote)
	head := runGit(t, source, "rev-parse", "HEAD")
	upstream := "https://github.com/example/standards.git"
	fake := &fakeCodex{Changes: 1, SkipValidation: true, RemoteAlias: map[string]string{upstream: remote}, CIResponse: string(mustJSON(t, map[string]any{"workflow_runs": []any{map[string]any{"head_sha": head, "path": ".github/workflows/ci.yml", "status": "completed", "conclusion": "failure"}}}))}
	c := Default(Entry{Path: source, Remote: upstream, Branch: "main", Role: "source"})
	report, err := (Maintainer{Commands: fake}).Run(context.Background(), c, filepath.Join(root, "state"))
	if err == nil || !strings.Contains(err.Error(), "CI did not succeed") {
		t.Fatalf("remote CI bypass: %v", err)
	}
	if report.Executable != "" || !report.LearnedAt.IsZero() {
		t.Fatal("unchecked source promoted")
	}
	for _, call := range fake.Calls {
		if len(call) > 0 && call[0] == "go" {
			t.Fatal("unchecked bundle built for rollout")
		}
	}
	if tags := runGit(t, remote, "tag"); tags != "" {
		t.Fatal("unchecked release tagged")
	}
}

func TestCopySourceSkipsWorktreeGitFile(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	put(t, source, ".git", "gitdir: /original/repository/worktrees/checkout\n")
	put(t, source, "README.md", "# source\n")
	copySource(t, source, target)
	if _, err := os.Stat(filepath.Join(target, ".git")); !os.IsNotExist(err) {
		t.Fatal("copied worktree Git pointer")
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatal(err)
	}
}
