package engine

import (
	"fmt"
	"path"
	"strings"

	"github.com/BurntSushi/toml"
)

func componentFile(c Component, name string) string { return path.Join(c.Path, name) }

func validatePythonTools(cfg map[string]any) error {
	tools, ok := cfg["tools"].(map[string]any)
	if !ok {
		return fmt.Errorf("python-uv requires an explicit tools table with pinned python and uv")
	}
	for _, name := range []string{"python", "uv"} {
		version, ok := tools[name].(string)
		if !ok || !pinnedGo(version) {
			return fmt.Errorf("tools.%s must be an exact x.y.z version", name)
		}
	}
	return nil
}

func validatePythonInputs(p *Plan, c Component) error {
	project := p.Inputs[componentFile(c, "pyproject.toml")]
	lock := p.Inputs[componentFile(c, "uv.lock")]
	if !project.Exists || !lock.Exists || strings.TrimSpace(lock.Content) == "" {
		return fmt.Errorf("python-uv requires pyproject.toml and an existing uv.lock in the component directory")
	}
	var cfg struct {
		Project struct {
			Name string `toml:"name"`
		} `toml:"project"`
		Build struct {
			Backend string `toml:"build-backend"`
		} `toml:"build-system"`
		Tool struct {
			UV struct {
				Workspace map[string]any `toml:"workspace"`
			} `toml:"uv"`
		} `toml:"tool"`
	}
	if _, err := toml.Decode(project.Content, &cfg); err != nil {
		return fmt.Errorf("pyproject.toml: %w", err)
	}
	if cfg.Project.Name == "" || cfg.Build.Backend == "" || cfg.Tool.UV.Workspace != nil {
		return fmt.Errorf("python-uv supports one buildable package, without a uv workspace")
	}
	var parsedLock map[string]any
	if _, err := toml.Decode(lock.Content, &parsedLock); err != nil {
		return fmt.Errorf("uv.lock: %w", err)
	}
	if _, ok := parsedLock["version"].(int64); !ok {
		return fmt.Errorf("uv.lock must declare an integer version")
	}
	return nil
}

// Project extensions add checks without replacing the required language tasks.
func componentTasks(c Component, project string) string {
	base := tasks()
	if c.Profile == "python-uv" {
		base = fmt.Sprintf(`[bootstrap]
run = "uv sync --locked --project %s"

[lint]
run = "uv run --locked --project %s ruff check %s"

[format]
run = "uv run --locked --project %s ruff format %s"

["format:check"]
run = "uv run --locked --project %s ruff format --check %s"

[test]
run = "uv run --locked --project %s pytest %s"

[build]
run = "uv build --project %s --out-dir %s"

[check]
depends = ["standards:check", "lint", "format:check", "test", "build"]

["standards:check"]
run = "dev-standards check"

["standards:plan"]
run = "dev-standards plan"

["standards:verify"]
run = "dev-standards verify"
`, c.Path, c.Path, c.Path, c.Path, c.Path, c.Path, c.Path, c.Path, c.Path, c.Path, componentFile(c, "dist"))
	}
	var extensions map[string]any
	_, _ = toml.Decode(project, &extensions) // Plan validates the same source first.
	for _, task := range []string{"bootstrap", "lint", "format", "format:check", "test", "build"} {
		extension := "project:" + task
		if _, ok := extensions[extension]; !ok {
			continue
		}
		header := "[" + task + "]\n"
		if strings.Contains(task, ":") {
			header = "[\"" + task + "\"]\n"
		}
		base = strings.Replace(base, header, header+fmt.Sprintf("depends = [%q]\n", extension), 1)
	}
	return base
}

func componentWorkflow(c Component) string {
	if c.Profile != "python-uv" {
		return projectWorkflow
	}
	workflow := strings.Replace(projectWorkflow, "  workflow-result:", "  standards-check:", 1)
	return strings.Replace(workflow, "      - run: gofmt -l . > /tmp/formatting && test ! -s /tmp/formatting", "      - run: mise run format:check", 1)
}
