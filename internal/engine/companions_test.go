package engine

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func companionFixture(t *testing.T) (*Tree, Request) {
	t.Helper()
	tree, req := nodeFixture(t)
	req.Instructions = ""
	req.InstructionLanguage = "en"
	req.CompanionsPath = "companions.json"
	write(t, tree.Root, "mise.toml", "[tools]\nnode=\"24.21.0\"\ngo=\"1.27.3\"\n")
	write(t, tree.Root, "companions.json", `[{"path":"harness","profile":"go","verifyTask":"project:verify:harness"},{"path":"apple","profile":"swift","verifyTask":"project:verify:apple"}]`)
	write(t, tree.Root, "harness/go.mod", "module example.com/harness\n\ngo 1.27.3\n")
	write(t, tree.Root, "apple/Package.swift", "// swift-tools-version: 6.0\n")
	write(t, tree.Root, ".standards/tasks.project.toml", "[\"project:verify:harness\"]\nrun='echo harness'\n[\"project:verify:apple\"]\nrun='echo apple'\n")
	return tree, req
}

func TestCompanionAdoptionRollbackReapplyAndConflict(t *testing.T) {
	tree, req := companionFixture(t)
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	original := read(t, tree.Root, "mise.toml")
	injected := errors.New("write failure")
	if err := tree.apply(plan, func(i int) error {
		if i == 6 {
			return injected
		}
		return nil
	}); !errors.Is(err, injected) {
		t.Fatalf("rollback: %v", err)
	}
	if read(t, tree.Root, "mise.toml") != original {
		t.Fatal("failed adoption changed mise")
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Check(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, tree.Root, "AGENTS.md"), "## Shared development standards") {
		t.Fatal("English instructions missing")
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if !strings.Contains(workflow, "runs-on: macos-26") || !strings.Contains(workflow, "mise run standards:companions") {
		t.Fatal("companion CI missing")
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("not idempotent: %+v %v", again, err)
	}
	write(t, tree.Root, "mise.toml", original+"# human edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) == 0 {
		t.Fatalf("missing conflict: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("overwrote human edit")
	}
}

func TestCompanionRejectsIncompleteOrUnsafeDeclarations(t *testing.T) {
	for _, variant := range []string{"undeclared", "missing-manifest", "duplicate", "traversal", "template", "unknown-profile", "missing-task", "skipped-task", "cached-task", "dependency-cycle", "empty-run", "floating-go", "ignored-marker", "stale-plan", "symlink"} {
		t.Run(variant, func(t *testing.T) {
			tree, req := companionFixture(t)
			switch variant {
			case "undeclared":
				write(t, tree.Root, "other/go.mod", "module extra\n")
			case "missing-manifest":
				os.Remove(filepath.Join(tree.Root, "harness/go.mod"))
			case "duplicate":
				write(t, tree.Root, "companions.json", `[{"path":"harness","profile":"go","verifyTask":"project:verify:a"},{"path":"harness","profile":"go","verifyTask":"project:verify:b"}]`)
			case "traversal":
				write(t, tree.Root, "companions.json", `[{"path":"../harness","profile":"go","verifyTask":"project:verify:a"}]`)
			case "template":
				write(t, tree.Root, "companions.json", `[{"path":"{{env.SECRET}}","profile":"go","verifyTask":"project:verify:a"}]`)
			case "unknown-profile":
				write(t, tree.Root, "companions.json", `[{"path":"harness","profile":"rust","verifyTask":"project:verify:a"}]`)
			case "missing-task":
				write(t, tree.Root, ".standards/tasks.project.toml", "")
			case "skipped-task":
				write(t, tree.Root, ".standards/tasks.project.toml", read(t, tree.Root, ".standards/tasks.project.toml")+"platforms=['linux']\n")
			case "cached-task":
				write(t, tree.Root, ".standards/tasks.project.toml", read(t, tree.Root, ".standards/tasks.project.toml")+"sources=['apple/**']\n")
			case "dependency-cycle":
				write(t, tree.Root, ".standards/tasks.project.toml", read(t, tree.Root, ".standards/tasks.project.toml")+"depends=['check']\n")
			case "empty-run":
				write(t, tree.Root, ".standards/tasks.project.toml", "[\"project:verify:harness\"]\nrun=''\n")
			case "floating-go":
				write(t, tree.Root, "mise.toml", "[tools]\nnode=\"24.21.0\"\ngo=\"1.27\"\n")
			case "ignored-marker":
				write(t, tree.Root, ".gitignore", "harness/\n")
			case "stale-plan":
				plan, err := tree.Plan(req)
				if err != nil {
					t.Fatal(err)
				}
				write(t, tree.Root, "harness/go.mod", "module changed\n")
				if err := tree.Apply(plan); err == nil {
					t.Fatal("accepted changed companion manifest")
				}
				return
			case "symlink":
				if runtime.GOOS == "windows" {
					t.Skip("symlink privileges")
				}
				os.Remove(filepath.Join(tree.Root, "harness/go.mod"))
				if err := os.Symlink(filepath.Join(tree.Root, "package.json"), filepath.Join(tree.Root, "harness/go.mod")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tree.Plan(req); err == nil {
				t.Fatal("accepted unsafe companion configuration")
			}
		})
	}
}

func TestCompanionFailurePreventsVerificationReceipt(t *testing.T) {
	// Exercise the real mise dependency graph, not a mock of task execution.
	if runtime.GOOS == "windows" {
		t.Skip("shell exit status fixture")
	}
	if _, err := exec.LookPath("mise"); err != nil {
		t.Skip("mise unavailable")
	}
	tree, req := companionFixture(t)
	// A Go-only companion can be verified on every supported host.
	os.RemoveAll(filepath.Join(tree.Root, "apple"))
	write(t, tree.Root, "companions.json", `[{"path":"harness","profile":"go","verifyTask":"project:verify:harness"}]`)
	write(t, tree.Root, "package.json", `{"scripts":{"lint":"node -e \"\"","format":"node -e \"\"","format:check":"node -e \"\"","test":"node -e \"\"","build":"node -e \"\""}}`)
	write(t, tree.Root, ".standards/tasks.project.toml", "[\"project:verify:harness\"]\nrun='exit 7'\n")
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISE_YES", "1")
	cmd := exec.Command("mise", "trust", tree.Root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trust: %s %v", output, err)
	}
	// Both the aggregate task and Verify must propagate the companion failure.
	cmd = exec.Command("mise", "run", "standards:companions")
	cmd.Dir = tree.Root
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("aggregate ignored failure: %s", output)
	}
	r, err := tree.Verify(os.Stderr)
	if err == nil || tree.ReceiptValid() {
		t.Fatal("companion failure produced successful verification")
	}
	last := r.Results[len(r.Results)-1]
	if last.Component != "harness" || last.Status != "fail" {
		t.Fatalf("failure not attributed: %+v", last)
	}
}

func TestEnglishLanguageDecision(t *testing.T) {
	if err := languageDecisionFor("# Language\n\n## Requirements\n\nA browser application with typed interfaces.\n\n## Candidates\n\nTypeScript and JavaScript are candidates.\n\n## Selection rationale\n\nTypeScript provides static checking in the browser.\n", "node-npm"); err != nil {
		t.Fatal(err)
	}
}
