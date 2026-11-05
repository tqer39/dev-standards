package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

type workflowDocument struct {
	On          any               `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func parseWorkflow(t *testing.T, content string) workflowDocument {
	t.Helper()
	var document workflowDocument
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func adoptWithAutomation(t *testing.T) *Tree {
	t.Helper()
	tree := fixture(t)
	plan, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	return tree
}

// The main workflow's runs are matched by SHA and path while maintenance waits
// for CI, so a lag failure must stay in its own file to avoid blocking rollout.
func TestVersionLagWorkflowStaysSeparateAndReadOnly(t *testing.T) {
	tree := adoptWithAutomation(t)
	content := read(t, tree.Root, versionLagWorkflowPath)
	document := parseWorkflow(t, content)
	events, ok := document.On.(map[string]any)
	if !ok || len(events) != 2 || events["schedule"] == nil {
		t.Fatalf("events must be limited to schedule and workflow_dispatch: %v", document.On)
	}
	if _, ok := events["workflow_dispatch"]; !ok {
		t.Fatalf("manual run is missing: %v", document.On)
	}
	if !reflect.DeepEqual(document.Permissions, map[string]string{"contents": "read"}) {
		t.Fatalf("permissions must stay read-only: %v", document.Permissions)
	}
	for _, forbidden := range []string{"secrets.", "github.token", "GH_TOKEN"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("lag check must not use credentials: %s", forbidden)
		}
	}
	if !strings.Contains(content, "persist-credentials: false") {
		t.Fatal("checkout keeps credentials")
	}
	if _, ok := document.Jobs["workflow-result"]; ok {
		t.Fatal("lag check must not reuse the required job name")
	}
	main := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if strings.Contains(main, "schedule") {
		t.Fatal("main workflow gained a schedule")
	}
}

func TestVersionLagWorkflowIsManagedAndIdempotent(t *testing.T) {
	tree := adoptWithAutomation(t)
	if !strings.Contains(read(t, tree.Root, ".gitattributes"), "/"+versionLagWorkflowPath+" text eol=lf\n") {
		t.Fatal("line endings are not fixed")
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("not idempotent: %+v %v", again, err)
	}
	content := read(t, tree.Root, versionLagWorkflowPath)
	write(t, tree.Root, versionLagWorkflowPath, content+"# human edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("human edit is not protected: %+v %v", conflict, err)
	}
}

// Rewrites an adopted tree to what a version without the lag workflow left behind.
// The bundle digest stays as is: it covers rules and templates, so a release that
// only changes the CLI, like the one adding this workflow, keeps the same digest.
func downgradeToStateWithoutVersionLag(t *testing.T, tree *Tree) {
	t.Helper()
	const previousVersion = "0.4.7"
	var state State
	if err := json.Unmarshal([]byte(read(t, tree.Root, ".standards/state.json")), &state); err != nil {
		t.Fatal(err)
	}
	delete(state.Files, versionLagWorkflowPath)
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	var lock map[string]any
	if err := json.Unmarshal([]byte(read(t, tree.Root, ".standards/lock.json")), &lock); err != nil {
		t.Fatal(err)
	}
	lock["version"] = previousVersion
	write(t, tree.Root, ".standards/lock.json", string(JSON(lock)))
	var manifest Manifest
	if err := json.Unmarshal([]byte(read(t, tree.Root, ".standards/repository.yml")), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Standard.Version = previousVersion
	write(t, tree.Root, ".standards/repository.yml", string(JSON(manifest)))
	if err := os.Remove(filepath.Join(tree.Root, versionLagWorkflowPath)); err != nil {
		t.Fatal(err)
	}
	commit(t, tree.Root)
}

func TestUpgradeAddsVersionLagWorkflowToExistingProject(t *testing.T) {
	for name, tree := range map[string]*Tree{"AGENTS.md": adoptWithAutomation(t), "rulesync": adoptNodeRulesync(t)} {
		t.Run(name, func(t *testing.T) {
			downgradeToStateWithoutVersionLag(t, tree)
			plan, err := tree.Plan(Request{Mode: "upgrade", Automation: true})
			if err != nil {
				t.Fatalf("every existing project would stop rolling out: %v", err)
			}
			if len(plan.Conflicts) != 0 {
				t.Fatalf("conflicts: %v", plan.Conflicts)
			}
			if err := tree.Apply(plan); err != nil {
				t.Fatal(err)
			}
			if read(t, tree.Root, versionLagWorkflowPath) != versionLagWorkflow {
				t.Fatal("workflow was not added")
			}
			commit(t, tree.Root)
			again, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
				t.Fatalf("not idempotent: %+v %v", again, err)
			}
		})
	}
}

func TestUpgradeKeepsExistingUnownedVersionLagWorkflow(t *testing.T) {
	tree := adoptWithAutomation(t)
	downgradeToStateWithoutVersionLag(t, tree)
	write(t, tree.Root, versionLagWorkflowPath, "name: Existing\n")
	if _, err := tree.Plan(Request{Mode: "upgrade", Automation: true}); err == nil {
		t.Fatal("overwrote an existing workflow")
	}
	if read(t, tree.Root, versionLagWorkflowPath) != "name: Existing\n" {
		t.Fatal("existing workflow changed")
	}
}

// Only a state written by an earlier version may lack the entry.
func TestCurrentStateMustOwnVersionLagWorkflow(t *testing.T) {
	tree := adoptWithAutomation(t)
	var state State
	if err := json.Unmarshal([]byte(read(t, tree.Root, ".standards/state.json")), &state); err != nil {
		t.Fatal(err)
	}
	delete(state.Files, versionLagWorkflowPath)
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	commit(t, tree.Root)
	if _, err := tree.Plan(Request{Mode: "reapply"}); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("tampered state accepted: %v", err)
	}
}

func TestVersionLagWorkflowRequiresAutomation(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	if _, err := os.Stat(filepath.Join(tree.Root, versionLagWorkflowPath)); !os.IsNotExist(err) {
		t.Fatalf("basic adoption has no maintenance to watch: %v", err)
	}
}

func TestVersionLagWorkflowRejectsUnownedFile(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, versionLagWorkflowPath, "name: Existing\n")
	if _, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true}); err == nil {
		t.Fatal("overwrote an existing workflow")
	}
}

// Runs the generated step because a missing path in its required set would fail
// every project's CI, which no string assertion on the workflow can show.
func TestMainWorkflowAcceptsVersionLagWorkflowAsManaged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated steps run on GitHub-hosted Linux")
	}
	for name, tree := range map[string]*Tree{"AGENTS.md": adoptWithAutomation(t), "rulesync": adoptNodeRulesync(t)} {
		t.Run(name, func(t *testing.T) {
			document := parseWorkflow(t, read(t, tree.Root, ".github/workflows/dev-standards.yml"))
			job, ok := document.Jobs["workflow-result"]
			if !ok {
				job = document.Jobs["standards-check"]
			}
			for _, step := range job.Steps {
				if step.Name == "Confirm managed configuration" {
					if output, err := runWorkflowStep(t, tree.Root, step.Run, nil); err != nil {
						t.Fatalf("%v: %s", err, output)
					}
					return
				}
			}
			t.Fatal("managed configuration step is missing")
		})
	}
}

func adoptNodeRulesync(t *testing.T) *Tree {
	t.Helper()
	tree, req := nodeFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	return tree
}

func runWorkflowStep(t *testing.T, dir, script string, env []string) (string, error) {
	t.Helper()
	name := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(name, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "-e", name)
	command.Dir = dir
	// A mise shim would load the untrusted fixture config; CI uses the runner's Python.
	var entries []string
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if !strings.Contains(entry, filepath.Join("mise", "shims")) {
			entries = append(entries, entry)
		}
	}
	command.Env = append(os.Environ(), append(env, "PATH="+strings.Join(entries, string(os.PathListSeparator)))...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func gitIn(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	command.Dir = dir
	command.Env = append(os.Environ(), env...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

const day = 24 * time.Hour

func TestVersionLagDetection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the workflow runs on GitHub-hosted Linux")
	}
	type tag struct {
		name string
		age  time.Duration
	}
	cases := []struct {
		name    string
		applied string
		tags    []tag
		failure string
	}{
		{name: "latest version applied", applied: "1.2.0", tags: []tag{{"v1.1.0", 30 * day}, {"v1.2.0", 30 * day}}},
		{name: "newer version within grace period", applied: "1.2.0", tags: []tag{{"v1.2.0", 30 * day}, {"v1.3.0", day}}},
		{name: "newer version past grace period", applied: "1.2.0", tags: []tag{{"v1.2.0", 30 * day}, {"v1.3.0", 8 * day}}, failure: "1.3.0"},
		// A fresh latest tag must not reset the clock while the host stays stopped.
		{name: "oldest unapplied version decides", applied: "1.2.0", tags: []tag{{"v1.3.0", 10 * day}, {"v1.4.0", day}}, failure: "1.3.0"},
		{name: "versions compare numerically", applied: "1.10.0", tags: []tag{{"v1.9.0", 30 * day}, {"v1.10.0", 30 * day}}},
		{name: "applied version ahead of tags", applied: "1.3.0", tags: []tag{{"v1.2.0", 30 * day}}},
		{name: "other tag formats ignored", applied: "1.2.0", tags: []tag{{"v1.2.0", 30 * day}, {"v1.3.0-rc1", 30 * day}, {"latest", 30 * day}}},
		{name: "malformed applied version", applied: "main", tags: []tag{{"v1.2.0", 30 * day}}, failure: "main"},
		// An empty result would otherwise read as up to date while the check is blind.
		{name: "no published versions", applied: "1.2.0", tags: []tag{{"latest", 30 * day}}, failure: "no published"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			upstream := t.TempDir()
			gitIn(t, upstream, nil, "init", "--quiet")
			for _, tag := range tt.tags {
				date := time.Now().Add(-tag.age).Format(time.RFC3339)
				gitIn(t, upstream, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, "commit", "--allow-empty", "-qm", tag.name)
				gitIn(t, upstream, nil, "tag", tag.name)
			}
			output, err := runVersionLagCheck(t, tt.applied, "file://"+upstream)
			if tt.failure == "" {
				if err != nil {
					t.Fatalf("%v: %s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, tt.failure) {
				t.Fatalf("expected failure with %q, got %v: %s", tt.failure, err, output)
			}
		})
	}
}

func TestVersionLagFailsWhenSourceIsUnreachable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the workflow runs on GitHub-hosted Linux")
	}
	if output, err := runVersionLagCheck(t, "1.2.0", "file://"+filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatalf("an unreadable source was reported as up to date: %s", output)
	}
}

func runVersionLagCheck(t *testing.T, applied, source string) (string, error) {
	t.Helper()
	document := parseWorkflow(t, versionLagWorkflow)
	steps := document.Jobs["version-lag"].Steps
	if len(steps) == 0 {
		t.Fatal("version-lag job is missing")
	}
	checkout := t.TempDir()
	gitIn(t, checkout, nil, "init", "--quiet")
	write(t, checkout, ".standards/lock.json", `{"version": "`+applied+`"}`)
	return runWorkflowStep(t, checkout, steps[len(steps)-1].Run, []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=url." + source + ".insteadOf",
		"GIT_CONFIG_VALUE_0=https://github.com/tqer39/dev-standards.git",
	})
}
