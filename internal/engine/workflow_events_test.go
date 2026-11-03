package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestPushBranchesRegenerationPreservesChecksAndOwnership(t *testing.T) {
	tree, req := nodeFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	const workflowPath = ".github/workflows/dev-standards.yml"
	original := read(t, tree.Root, workflowPath)
	if !strings.Contains(original, "on: [push, pull_request]\n") {
		t.Fatal("default events changed")
	}
	manifest := read(t, tree.Root, ".standards/repository.yml")
	configured := strings.Replace(manifest, `"prBudgetSeconds":`, `"pushBranches": ["trunk", "release/v1.0"], "prBudgetSeconds":`, 1)
	write(t, tree.Root, ".standards/repository.yml", configured)
	plan, err = tree.Plan(Request{Mode: "reapply"})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("write failure")
	if err := tree.apply(plan, func(i int) error {
		if i == 1 {
			return injected
		}
		return nil
	}); !errors.Is(err, injected) {
		t.Fatalf("rollback: %v", err)
	}
	if read(t, tree.Root, workflowPath) != original {
		t.Fatal("failed update changed workflow")
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	workflow := read(t, tree.Root, workflowPath)
	events := "on:\n  push:\n    branches: [\"trunk\", \"release/v1.0\"]\n  pull_request:\n  workflow_dispatch:\n"
	if !strings.Contains(workflow, events) {
		t.Fatalf("missing events: %s", workflow)
	}
	if strings.Replace(workflow, events, "on: [push, pull_request]\n", 1) != original {
		t.Fatal("changed checks, permissions, or job names")
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("not idempotent: %+v %v", again, err)
	}
	write(t, tree.Root, workflowPath, workflow+"# human edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("missing conflict: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("overwrote conflict")
	}
	if read(t, tree.Root, workflowPath) != workflow+"# human edit\n" {
		t.Fatal("human edit lost")
	}
	for _, invalid := range []string{`[]`, `null`, `["main","main"]`, `["*"]`, `["!main"]`, `["main\nrun: bad"]`, `["${{ github.ref }}"]`, `["../main"]`, `["main..next"]`, `["main.lock"]`, `["main."]`, `["main//next"]`, `["main.lock/next"]`, `"main"`} {
		write(t, tree.Root, ".standards/repository.yml", strings.Replace(manifest, `"prBudgetSeconds":`, `"pushBranches": `+invalid+`, "prBudgetSeconds":`, 1))
		if _, err := tree.Plan(Request{Mode: "reapply"}); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
		if read(t, tree.Root, workflowPath) != workflow+"# human edit\n" {
			t.Fatal("invalid plan wrote workflow")
		}
	}
}

func TestPushBranchesPreservesEveryProfileWorkflow(t *testing.T) {
	for _, profile := range []string{"go", "node-npm", "python-uv", "mise-project"} {
		t.Run(profile, func(t *testing.T) {
			m := defaultManifest()
			m.Components[0].Profile = profile
			original := componentWorkflow(m, "")
			m.CI.PushBranches = []string{"main"}
			got := componentWorkflow(m, "")
			events := "on:\n  push:\n    branches: [\"main\"]\n  pull_request:\n  workflow_dispatch:"
			if !strings.Contains(got, events) || strings.Replace(got, events, "on: [push, pull_request]", 1) != original {
				t.Fatal("profile checks changed")
			}
		})
	}
}
