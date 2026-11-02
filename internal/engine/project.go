package engine

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// mise-project delegates language-specific behavior to explicit repository tasks.
// It never infers that a particular package manager covers every component.
func validateMiseProject(cfg map[string]any) error {
	tools, ok := cfg["tools"].(map[string]any)
	if !ok || len(tools) == 0 {
		return fmt.Errorf("mise-project requires pinned tools")
	}
	for name, value := range tools {
		version, ok := value.(string)
		if !ok || version == "" || version == "latest" || version == "system" || version == "lts" || strings.ContainsAny(version, "*<>=^~ ") {
			return fmt.Errorf("tools.%s must be pinned explicitly", name)
		}
	}
	for _, name := range []string{"node", "python", "go", "uv", "pnpm"} {
		if value, exists := tools[name]; exists {
			version, ok := value.(string)
			if !ok || !pinnedGo(version) {
				return fmt.Errorf("tools.%s must be an exact x.y.z version", name)
			}
		}
	}
	tasks, _ := cfg["tasks"].(map[string]any)
	for _, name := range []string{"bootstrap", "lint", "format", "format:check", "test", "build"} {
		task, ok := tasks["repo:"+name].(map[string]any)
		if !ok {
			return fmt.Errorf("mise-project requires explicit repo:%s task", name)
		}
		for _, key := range []string{"platforms", "sources", "outputs", "skip", "wait_for"} {
			if _, exists := task[key]; exists {
				return fmt.Errorf("repo:%s cannot skip required checks with %s", name, key)
			}
		}
		run, hasRun := task["run"]
		deps, hasDeps := task["depends"]
		if (!hasRun || fmt.Sprint(run) == "" || fmt.Sprint(run) == "[]") && (!hasDeps || fmt.Sprint(deps) == "[]") {
			return fmt.Errorf("repo:%s must have a command or dependencies", name)
		}
	}
	return nil
}

func projectTasks() string {
	var b strings.Builder
	for _, name := range []string{"bootstrap", "lint", "format", "format:check", "test", "build"} {
		header := name
		if strings.Contains(name, ":") {
			header = fmt.Sprintf("%q", name)
		}
		fmt.Fprintf(&b, "[%s]\nrun = %q\n\n", header, "mise run repo:"+name)
	}
	b.WriteString(`[check]
depends = ["standards:check", "lint", "format:check", "test", "build"]

["standards:check"]
run = "dev-standards check"

["standards:plan"]
run = "dev-standards plan"

["standards:verify"]
run = "dev-standards verify"
`)
	return b.String()
}

// Include existing manifests and lockfiles in plan replay protection. Their
// interpretation remains the responsibility of the repository's locked tasks.
func (t *Tree) projectInputs() ([]string, error) {
	var names []string
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
		switch path.Base(p) {
		case "package.json", "package-lock.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "pyproject.toml", "uv.lock", "go.mod", "go.sum", "Package.swift", "Package.resolved", "Cargo.toml", "Cargo.lock":
			names = append(names, p)
		}
		return nil
	})
	if err == nil && len(names) == 0 {
		return nil, fmt.Errorf("mise-project requires existing project manifests")
	}
	return names, err
}
