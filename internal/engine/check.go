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
		return r, fmt.Errorf("repository.yml: %w", err)
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
		if rule.ID == dependencyIntakeRule || rule.ID == lockfileSourceRule {
			// 生成物の一致ではなく、利用先の設定と lock の中身で判定する。
			continue
		}
		if rule.Checks[0].Type == "review" {
			r.Results = append(r.Results, Result{rule.ID, rule.Checks[0].ID, component, "review_required", "not evaluated by check; compare the repository with the rule guidance"})
			continue
		}
		r.Results = append(r.Results, Result{rule.ID, "configuration", component, status, evidence})
	}
	for _, rule := range []string{"CORE-LINT", "CORE-FORMAT", "CORE-BOOTSTRAP"} {
		r.Results = append(r.Results, Result{rule, "execution", component, "not_run", "configuration check does not execute project tasks"})
	}
	r.Results = append(r.Results, Result{"PROCESS-LANGUAGE", "judgment", component, "review_required", "decision structure checked; suitability needs review"})
	dependencies := t.dependencyResults(p, component)
	r.Results = append(r.Results, dependencies...)
	if status == "fail" {
		return r, fail(1, "configuration drift detected")
	}
	for _, result := range dependencies {
		if result.Status == "fail" {
			return r, fail(1, "%s %s: %s", result.Rule, result.Check, result.Evidence)
		}
	}
	return r, nil
}

// Verify executes the repository tasks explicitly, without claiming a review
// of the language decision or a clean-machine bootstrap from static evidence.
func (t *Tree) Verify(log io.Writer) (Report, error) {
	return t.verify(t.Check, func(task string) error {
		cmd := exec.Command("mise", "run", task)
		cmd.Dir = t.Root
		cmd.Stdout = log
		cmd.Stderr = log
		return cmd.Run()
	})
}

// The receipt vouches for the tree that Check judged, so the starting
// fingerprint is taken before Check reads anything. An edit made while Check
// or the tasks run then differs from the fingerprint WriteReceipt takes at the
// end. A fingerprint error is reported after the result of Check, which says
// more about a broken tree.
func (t *Tree) verify(check func() (Report, error), run func(task string) error) (Report, error) {
	fingerprint, fingerprintErr := t.Fingerprint()
	r, err := check()
	if err != nil {
		return r, err
	}
	if fingerprintErr != nil {
		return r, fingerprintErr
	}
	var manifest Manifest
	snapshot, err := t.Read(".standards/repository.yml")
	if err != nil {
		return r, err
	}
	if err := bundle.Decode([]byte(snapshot.Content), "repository", &manifest); err != nil {
		return r, fmt.Errorf("repository.yml: %w", err)
	}
	if err := companionHostSupported(manifest); err != nil {
		return r, err
	}
	r.Scope = "configuration-and-project-tasks"
	tasks := []string{"lint", "format:check", "test", "build"}
	for _, c := range manifest.Companions {
		tasks = append(tasks, c.VerifyTask)
	}
	for _, task := range tasks {
		result := Result{"CORE-LINT", task, r.Results[0].Component, "pass", "mise run " + task}
		for _, c := range manifest.Companions {
			if task == c.VerifyTask {
				result.Component = c.Path
			}
		}
		if task == "format:check" {
			result.Rule = "CORE-FORMAT"
		}
		if err := run(task); err != nil {
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
			return inputError(p, s.Content, err)
		}
		if !bytes.Equal(formatted, []byte(s.Content)) {
			changed = append(changed, p)
		}
		return nil
	})
	return changed, err
}

type Inspection struct {
	Root      string           `json:"root"`
	HEAD      string           `json:"head"`
	Files     []string         `json:"configurationFiles"`
	Languages []string         `json:"languages"`
	GitStatus string           `json:"gitStatus"`
	Adopted   bool             `json:"adopted"`
	Standard  *AppliedStandard `json:"standard,omitempty"`
}

func (t *Tree) Inspect() (Inspection, error) {
	r := Inspection{Root: t.Root, HEAD: t.head(), Files: []string{}, Languages: []string{}}
	standard, err := t.AppliedStandard()
	if err != nil {
		return r, err
	}
	r.Standard = standard
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
	if r.Adopted && r.Standard == nil {
		return r, fmt.Errorf("adopted repository is missing .standards/lock.json")
	}
	err = fs.WalkDir(t.fs.FS(), ".", func(p string, d fs.DirEntry, err error) error {
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
