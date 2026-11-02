package engine

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

func companionMarker(c Companion) string {
	if c.Profile == "go" {
		return c.Path + "/go.mod"
	}
	return c.Path + "/Package.swift"
}

func validateCompanions(m Manifest) error {
	if len(m.Companions) == 0 {
		return nil
	}
	if len(m.Components) != 1 || m.Components[0].Profile != "node-npm" {
		return fmt.Errorf("companion packages require a node-npm primary component")
	}
	seen := map[string]bool{}
	seenTasks := map[string]bool{}
	for _, c := range m.Companions {
		if c.Profile != "go" && c.Profile != "swift" {
			return fmt.Errorf("unsupported companion profile: %s", c.Profile)
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_-]*(/[a-zA-Z0-9_][a-zA-Z0-9_-]*)*$`).MatchString(c.Path) {
			return fmt.Errorf("companion path must be a literal nested directory")
		}
		if c.Path == m.Components[0].Path || seen[c.Path] {
			return fmt.Errorf("duplicate component path: %s", c.Path)
		}
		seen[c.Path] = true
		if !regexp.MustCompile(`^project:verify:[a-z][a-z0-9-]*$`).MatchString(c.VerifyTask) {
			return fmt.Errorf("invalid companion verification task")
		}
		if seenTasks[c.VerifyTask] {
			return fmt.Errorf("duplicate companion verification task: %s", c.VerifyTask)
		}
		seenTasks[c.VerifyTask] = true
		if c.Profile == "swift" && strings.Join(m.Platforms, ",") != "macos" {
			return fmt.Errorf("Swift companion verification currently requires platforms: [macos]")
		}
	}
	return nil
}

func validateCompanionTasks(m Manifest, source string) error {
	var tasks map[string]any
	if _, err := toml.Decode(source, &tasks); err != nil {
		return err
	}
	for _, c := range m.Companions {
		task, ok := tasks[c.VerifyTask].(map[string]any)
		if !ok {
			return fmt.Errorf("define companion task %s in .standards/tasks.project.toml", c.VerifyTask)
		}
		// A success receipt must not be issued because mise skipped a companion.
		for _, key := range []string{"platforms", "sources", "outputs", "skip", "depends", "depends_post", "wait_for"} {
			if _, ok := task[key]; ok {
				return fmt.Errorf("companion task %s cannot use %s", c.VerifyTask, key)
			}
		}
		switch run := task["run"].(type) {
		case string:
			if strings.TrimSpace(run) == "" {
				return fmt.Errorf("empty companion task: %s", c.VerifyTask)
			}
		case []any:
			if len(run) == 0 {
				return fmt.Errorf("empty companion task: %s", c.VerifyTask)
			}
			for _, command := range run {
				if s, ok := command.(string); !ok || strings.TrimSpace(s) == "" {
					return fmt.Errorf("invalid companion command")
				}
			}
		default:
			return fmt.Errorf("companion task %s needs an explicit run command", c.VerifyTask)
		}
	}
	return nil
}

func tasksWithCompanions(m Manifest, source string) string {
	base := componentTasks(m.Components[0], source)
	if len(m.Companions) == 0 {
		return base
	}
	var tasks []string
	for _, c := range m.Companions {
		tasks = append(tasks, fmt.Sprintf("%q", c.VerifyTask))
	}
	base = strings.Replace(base, `depends = ["standards:check",`, `depends = ["standards:companions", "standards:check",`, 1)
	return base + "\n[\"standards:companions\"]\ndepends = [" + strings.Join(tasks, ", ") + "]\n"
}

func companionHostSupported(m Manifest) error {
	for _, c := range m.Companions {
		if c.Profile == "swift" && runtime.GOOS != "darwin" {
			return fmt.Errorf("Swift companion verification requires macOS")
		}
	}
	return nil
}
