package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

var nodeScripts = []string{"lint", "format", "format:check", "test", "build"}

func validateNodeTools(cfg map[string]any) error {
	tools, ok := cfg["tools"].(map[string]any)
	if !ok {
		return fmt.Errorf("node-npm requires an explicit tools table with pinned node")
	}
	version, ok := tools["node"].(string)
	if !ok || !pinnedGo(version) {
		return fmt.Errorf("tools.node must be an exact x.y.z version")
	}
	return nil
}

// The common tasks call npm scripts, so each project keeps its own linter,
// formatter and test runner while the task names stay identical.
func validateNodeInputs(p *Plan, c Component) error {
	project := p.Inputs[componentFile(c, "package.json")]
	lock := p.Inputs[componentFile(c, "package-lock.json")]
	if !project.Exists || !lock.Exists {
		return fmt.Errorf("node-npm requires package.json and an existing package-lock.json in the component directory")
	}
	var pkg struct {
		Workspaces any               `json:"workspaces"`
		Scripts    map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(project.Content), &pkg); err != nil {
		return inputError(componentFile(c, "package.json"), project.Content, err)
	}
	if pkg.Workspaces != nil {
		return fmt.Errorf("node-npm supports one package, without npm workspaces")
	}
	for _, name := range nodeScripts {
		if pkg.Scripts[name] == "" {
			return fmt.Errorf("package.json must define the %s script", name)
		}
	}
	var parsedLock struct {
		LockfileVersion int `json:"lockfileVersion"`
	}
	if err := json.Unmarshal([]byte(lock.Content), &parsedLock); err != nil {
		return inputError(componentFile(c, "package-lock.json"), lock.Content, err)
	}
	if parsedLock.LockfileVersion < 2 {
		return fmt.Errorf("package-lock.json must use lockfileVersion 2 or later")
	}
	return nil
}

// npmLockViolations lists the packages npm would fetch from anywhere but the
// public registry, or without a sha512 integrity. Bundled packages carry no
// fetch of their own, and neither does a link to a directory that local
// confirms is inside the repository.
func npmLockViolations(content string, local func(string) bool) ([]string, error) {
	var lock struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Resolved  string `json:"resolved"`
			Integrity string `json:"integrity"`
			Link      bool   `json:"link"`
			InBundle  bool   `json:"inBundle"`
		} `json:"packages"`
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(content), &lock); err != nil {
		return nil, err
	}
	if lock.LockfileVersion < 2 {
		// 版 1 には packages がなく、空の一覧を合格と誤って報告してしまう。
		return nil, fmt.Errorf("lockfileVersion 2 or later is required")
	}
	if lock.Packages == nil && len(lock.Dependencies) > 0 {
		// packages がないと、npm は旧形式の dependencies から依存を復元する。
		return nil, fmt.Errorf("packages is required beside the legacy dependencies")
	}
	var found []string
	for name, pkg := range lock.Packages {
		if name == "" || pkg.InBundle {
			continue
		}
		installed := strings.Contains("/"+name, "/node_modules/")
		switch {
		case pkg.Link:
			if !local(pkg.Resolved) {
				found = append(found, name)
			}
		case !installed && pkg.Resolved == "":
			// link の指す先。キーがそのディレクトリのパスになる。
			if !local(name) {
				found = append(found, name)
			}
		case !strings.HasPrefix(pkg.Resolved, npmRegistry) || !strings.HasPrefix(pkg.Integrity, "sha512-"):
			found = append(found, name)
		}
	}
	return found, nil
}

// Lifecycle scripts stay disabled so bootstrap does not run dependency code.
func nodeTasks(c Component) string {
	return fmt.Sprintf(`[bootstrap]
run = "npm --prefix %[1]s ci --ignore-scripts"

[lint]
run = "npm --prefix %[1]s run lint"

[format]
run = "npm --prefix %[1]s run format"

["format:check"]
run = "npm --prefix %[1]s run format:check"

[test]
run = "npm --prefix %[1]s run test"

[build]
run = "npm --prefix %[1]s run build"

[check]
depends = ["standards:check", "lint", "format:check", "test", "build"]

["standards:check"]
run = "dev-standards check"

["standards:plan"]
run = "dev-standards plan"

["standards:verify"]
run = "dev-standards verify"
`, c.Path)
}
