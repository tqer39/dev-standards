package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pythonFixture(t *testing.T) (*Tree, Request) {
	t.Helper()
	tree := fixture(t)
	for _, name := range []string{"go.mod", "main.go"} {
		if err := os.Remove(filepath.Join(tree.Root, name)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, tree.Root, "session-indexer/pyproject.toml", "[project]\nname = \"sample\"\n[build-system]\nbuild-backend = \"hatchling.build\"\n")
	write(t, tree.Root, "session-indexer/uv.lock", "version = 1\n")
	write(t, tree.Root, "mise.toml", "[tools]\npython = \"3.13.0\"\nuv = \"0.11.32\"\n[tasks.up]\nrun = \"docker compose up -d\"\n")
	write(t, tree.Root, ".editorconfig", "root = true\n[*]\nindent_style = space\n[*.py]\nindent_size = 4\n")
	write(t, tree.Root, ".gitignore", "*.jsonl\n!session-indexer/tests/fixtures/*.jsonl\n")
	write(t, tree.Root, ".standards/tasks.project.toml", "[\"project:lint\"]\nrun = \"echo original-hooks\"\n")
	return tree, Request{Mode: "adopt", Profile: "python-uv", ComponentPath: "session-indexer", DecisionPath: "decision.md", Automation: true}
}

func TestPythonAdoptReapplyConflictAndRollback(t *testing.T) {
	tree, req := pythonFixture(t)
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
		t.Fatal("existing mise was lost")
	}
	if _, err := os.Stat(filepath.Join(tree.Root, ".standards/state.json")); !os.IsNotExist(err) {
		t.Fatal("failed adoption left state")
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("non-idempotent: %+v %v", again, err)
	}
	if _, err := tree.Check(); err != nil {
		t.Fatal(err)
	}
	tasks := read(t, tree.Root, ".standards/tasks.standards.toml")
	for _, expected := range []string{"uv run --locked --project session-indexer ruff check session-indexer", "uv run --locked --project session-indexer pytest session-indexer", "uv build --project session-indexer --out-dir session-indexer/dist", `depends = ["project:lint"]`} {
		if !strings.Contains(tasks, expected) {
			t.Errorf("missing task: %s", expected)
		}
	}
	workflow := read(t, tree.Root, ".github/workflows/dev-standards.yml")
	if strings.Contains(workflow, "gofmt") || !strings.Contains(workflow, "mise run format:check") {
		t.Fatal("Go-specific workflow generated")
	}
	if !strings.Contains(read(t, tree.Root, ".standards/mise.project.toml"), "docker compose up") {
		t.Fatal("existing tasks lost")
	}
	write(t, tree.Root, ".editorconfig", "human edit\n")
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("missing conflict: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("overwrote human edit")
	}
}

func TestPythonRejectsUnsafeOrUnsupportedInputs(t *testing.T) {
	for _, variant := range []string{"missing-lock", "invalid-lock", "floating-python", "floating-uv", "extra-python", "go-module", "workspace", "traversal", "template", "option-path", "reserved-task", "ignored-lock", "changed-lock"} {
		t.Run(variant, func(t *testing.T) {
			tree, req := pythonFixture(t)
			switch variant {
			case "missing-lock":
				os.Remove(filepath.Join(tree.Root, "session-indexer/uv.lock"))
			case "invalid-lock":
				write(t, tree.Root, "session-indexer/uv.lock", "broken=[")
			case "floating-python":
				write(t, tree.Root, "mise.toml", "[tools]\npython='3.13'\nuv='0.11.32'\n")
			case "floating-uv":
				write(t, tree.Root, "mise.toml", "[tools]\npython='3.13.0'\nuv='latest'\n")
			case "extra-python":
				write(t, tree.Root, "other/pyproject.toml", "[project]\nname='other'\n")
			case "go-module":
				write(t, tree.Root, "go.mod", "module extra\n")
			case "workspace":
				write(t, tree.Root, "session-indexer/pyproject.toml", "[project]\nname='app'\n[build-system]\nbuild-backend='hatchling.build'\n[tool.uv.workspace]\nmembers=[]\n")
			case "traversal":
				req.ComponentPath = "../session-indexer"
			case "option-path":
				req.ComponentPath = "--help"
				if err := os.Rename(filepath.Join(tree.Root, "session-indexer"), filepath.Join(tree.Root, "--help")); err != nil {
					t.Fatal(err)
				}
			case "template":
				req.ComponentPath = "{{env.SECRET}}"
			case "reserved-task":
				write(t, tree.Root, "mise.toml", read(t, tree.Root, "mise.toml")+"[tasks.test]\nrun='echo bypass'\n")
			case "ignored-lock":
				write(t, tree.Root, ".gitignore", "*.lock\n")
			case "changed-lock":
				plan, err := tree.Plan(req)
				if err != nil {
					t.Fatal(err)
				}
				write(t, tree.Root, "session-indexer/uv.lock", "version=2\n")
				if err := tree.Apply(plan); err == nil {
					t.Fatal("accepted changed lock")
				}
				return
			}
			if _, err := tree.Plan(req); err == nil {
				t.Fatal("accepted unsupported input")
			}
		})
	}
}

func TestPythonRootComponent(t *testing.T) {
	tree, req := pythonFixture(t)
	for _, name := range []string{"pyproject.toml", "uv.lock"} {
		if err := os.Rename(filepath.Join(tree.Root, "session-indexer", name), filepath.Join(tree.Root, name)); err != nil {
			t.Fatal(err)
		}
	}
	req.ComponentPath = ""
	p, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.Apply(p); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Check(); err != nil {
		t.Fatal(err)
	}
}
