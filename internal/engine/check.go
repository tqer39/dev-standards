package engine

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os/exec"
	"strings"

	bundle "github.com/tqer39/dev-standards"
)

func (t *Tree) Check() (Report, error) {
	r := Report{HEAD: t.head(), Scope: "configuration-only", Results: []Result{}}
	p, err := t.Plan(Request{Mode: "reapply"})
	if err != nil {
		return r, err
	}
	var manifest Manifest
	if err := bundle.Decode([]byte(p.Inputs[".standards/repository.yml"].Content), "repository", &manifest); err != nil {
		return r, err
	}
	component := manifest.Components[0].Name
	r.Changes = p.Changes
	rules, err := bundle.Catalog()
	if err != nil {
		return r, err
	}
	status := "pass"
	evidence := "locked configuration matches deterministic generation"
	if len(p.Changes) > 0 || len(p.Conflicts) > 0 {
		status = "fail"
		evidence = fmt.Sprintf("%d pending changes; conflicts: %v", len(p.Changes), p.Conflicts)
	} else if manifest.AI.Instructions == "rulesync" {
		agents, err := t.Read("AGENTS.md")
		if err != nil {
			return r, err
		}
		if !strings.Contains(normalized(agents.Content), strings.TrimSpace(instructionsEntry(manifest))) {
			status = "fail"
			evidence = "AGENTS.md lacks the shared entry; run rulesync generate"
		}
	}
	for _, rule := range rules {
		r.Results = append(r.Results, Result{rule.ID, "configuration", component, status, evidence})
	}
	for _, rule := range []string{"CORE-LINT", "CORE-FORMAT", "CORE-BOOTSTRAP"} {
		r.Results = append(r.Results, Result{rule, "execution", component, "not_run", "configuration check does not execute project tasks"})
	}
	r.Results = append(r.Results, Result{"PROCESS-LANGUAGE", "judgment", component, "review_required", "decision structure checked; suitability needs review"})
	if status == "fail" {
		return r, fail(1, "configuration drift detected")
	}
	return r, nil
}

// Verify executes the repository tasks explicitly, without claiming a review
// of the language decision or a clean-machine bootstrap from static evidence.
func (t *Tree) Verify(log io.Writer) (Report, error) {
	r, err := t.Check()
	if err != nil {
		return r, err
	}
	fingerprint, err := t.Fingerprint()
	if err != nil {
		return r, err
	}
	r.Scope = "configuration-and-project-tasks"
	for _, task := range []string{"lint", "format:check", "test", "build"} {
		cmd := exec.Command("mise", "run", task)
		cmd.Dir = t.Root
		cmd.Stdout = log
		cmd.Stderr = log
		result := Result{"CORE-LINT", task, r.Results[0].Component, "pass", "mise run " + task}
		if task == "format:check" {
			result.Rule = "CORE-FORMAT"
		}
		if err := cmd.Run(); err != nil {
			result.Status = "fail"
			result.Evidence += " failed: " + err.Error()
			r.Results = append(r.Results, result)
			return r, fail(1, "project task failed: %s", task)
		}
		r.Results = append(r.Results, result)
	}
	if err := t.WriteReceipt(fingerprint); err != nil {
		return r, err
	}
	return r, nil
}

func (t *Tree) FormatCheck() ([]string, error) {
	var changed []string
	err := fs.WalkDir(t.fs.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		s, err := t.Read(p)
		if err != nil {
			return err
		}
		formatted, err := format.Source([]byte(s.Content))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if !bytes.Equal(formatted, []byte(s.Content)) {
			changed = append(changed, p)
		}
		return nil
	})
	return changed, err
}

type Inspection struct {
	Root      string   `json:"root"`
	HEAD      string   `json:"head"`
	Files     []string `json:"configurationFiles"`
	Languages []string `json:"languages"`
	GitStatus string   `json:"gitStatus"`
	Adopted   bool     `json:"adopted"`
}

func (t *Tree) Inspect() (Inspection, error) {
	r := Inspection{Root: t.Root, HEAD: t.head(), Files: []string{}, Languages: []string{}}
	for _, p := range []string{".editorconfig", ".gitignore", "mise.toml", "pyproject.toml", "go.mod", "package.json", "Cargo.toml", "Package.swift", "AGENTS.md", "rulesync.jsonc", "renovate.json", ".standards/repository.yml"} {
		s, err := t.Read(p)
		if err != nil {
			return r, err
		}
		if s.Exists {
			r.Files = append(r.Files, p)
		}
		if s.Exists {
			switch p {
			case "pyproject.toml":
				r.Languages = append(r.Languages, "python")
			case "go.mod":
				r.Languages = append(r.Languages, "go")
			case "package.json":
				r.Languages = append(r.Languages, "javascript-or-typescript")
			case "Cargo.toml":
				r.Languages = append(r.Languages, "rust")
			case "Package.swift":
				r.Languages = append(r.Languages, "swift")
			case ".standards/repository.yml":
				r.Adopted = true
			}
		}
	}
	err := fs.WalkDir(t.fs.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "vendor") {
			return fs.SkipDir
		}
		if d.IsDir() && p != "." {
			marker, err := fs.Stat(t.fs.FS(), p+"/pyvenv.cfg")
			if err == nil && !marker.IsDir() {
				return fs.SkipDir
			}
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".py") && !contains(r.Languages, "python") {
			r.Languages = append(r.Languages, "python")
		}
		if !d.IsDir() && d.Name() == "pyproject.toml" && p != "pyproject.toml" {
			if _, err := t.Read(p); err != nil {
				return err
			}
			r.Files = append(r.Files, p)
			if !contains(r.Languages, "python") {
				r.Languages = append(r.Languages, "python")
			}
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	b, err := exec.Command("git", "-C", t.Root, "status", "--porcelain=v1").Output()
	if err != nil {
		return r, err
	}
	r.GitStatus = string(b)
	return r, nil
}
