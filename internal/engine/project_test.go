package engine

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

func projectFixture(t *testing.T) (*Tree, Request) {
	t.Helper()
	tree := fixture(t)
	var config strings.Builder
	config.WriteString("[tools]\ngo = \"1.27.3\"\n")
	for _, name := range []string{"bootstrap", "lint", "format", "format:check", "test", "build"} {
		config.WriteString("[tasks.\"repo:" + name + "\"]\nrun = \"echo checked\"\n")
	}
	write(t, tree.Root, "mise.toml", config.String())
	write(t, tree.Root, "web/package.json", `{"name":"web"}`)
	write(t, tree.Root, "web/pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	write(t, tree.Root, "api/pyproject.toml", "[project]\nname = 'api'\n")
	write(t, tree.Root, "apple/Package.swift", "// swift-tools-version: 6.0\n")
	return tree, Request{Mode: "adopt", Profile: "mise-project", DecisionPath: "decision.md"}
}

func TestMiseProjectAdoptReplayAndFailure(t *testing.T) {
	tree, req := projectFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Inputs["web/pnpm-lock.yaml"].Exists {
		t.Fatal("lockfile absent from plan")
	}
	write(t, tree.Root, "web/pnpm-lock.yaml", "lockfileVersion: '9.1'\n")
	if err := tree.Apply(plan); err == nil {
		t.Fatal("stale lockfile plan applied")
	}
	plan, err = tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("reapply: %+v %v", again, err)
	}
	tasks := read(t, tree.Root, ".standards/tasks.standards.toml")
	write(t, tree.Root, ".standards/tasks.standards.toml", tasks+"# user edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("conflict: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("conflicting tasks applied")
	}
	write(t, tree.Root, ".standards/tasks.standards.toml", tasks)
	source := read(t, tree.Root, ".standards/mise.project.toml")
	write(t, tree.Root, ".standards/mise.project.toml", strings.Replace(source, "1.27.3", "1.27.4", 1))
	if _, err := tree.Plan(Request{Mode: "reapply"}); err == nil {
		t.Fatal("implicit tool update accepted")
	}
	write(t, tree.Root, ".standards/mise.project.toml", strings.Replace(source, "[tasks.\"repo:test\"]\nrun = \"echo checked\"", "[tasks.\"repo:test\"]\nrun = \"exit 1\"", 1))
	plan, err = tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("mise"); err != nil {
		t.Skip("mise required for task failure integration")
	}
	cmd := exec.Command("mise", "trust")
	cmd.Dir = tree.Root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trust: %s %v", out, err)
	}
	report, err := tree.Verify(io.Discard)
	if err == nil {
		t.Fatal("failed task reported success")
	}
	last := report.Results[len(report.Results)-1]
	if last.Check != "test" || last.Status != "fail" {
		t.Fatalf("wrong failure: %+v", last)
	}
}

func TestMiseProjectRejectsIncompleteTasksAndFloatingTools(t *testing.T) {
	for _, mutation := range []func(string) string{
		func(s string) string { return strings.Replace(s, "repo:build", "other:build", 1) },
		func(s string) string { return strings.Replace(s, "1.27.3", "latest", 1) },
		func(s string) string { return strings.Replace(s, "run = \"echo checked\"", "run = \"\"", 1) },
	} {
		tree, req := projectFixture(t)
		write(t, tree.Root, "mise.toml", mutation(read(t, tree.Root, "mise.toml")))
		if _, err := tree.Plan(req); err == nil {
			t.Fatal("invalid task configuration accepted")
		}
	}
}

func TestProjectEnglishDecisionAndGeneratedHeadingHierarchy(t *testing.T) {
	tree, req := projectFixture(t)
	write(t, tree.Root, "decision.md", "# Languages\n\n## Requirements\n\nMaintain the browser and harness together.\n\n## Candidates\n\nTypeScript and Go with existing libraries.\n\n## Rationale\n\nKeep TypeScript for the browser and Go for the harness.\n")
	write(t, tree.Root, "AGENTS.md", "# Project instructions\n\nPreserve existing behavior.\n")
	req.Automation = true
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", ".standards/rules.md"} {
		count := 0
		for _, line := range strings.Split(read(t, tree.Root, name), "\n") {
			if strings.HasPrefix(line, "# ") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%s has %d top-level headings", name, count)
		}
	}
}
