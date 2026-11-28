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
	// 設定の値は載せず、何番目の companion かで示す。
	for i, c := range m.Companions {
		if c.Profile != "go" && c.Profile != "swift" {
			return fmt.Errorf("unsupported companion profile in companion %d", i+1)
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_-]*(/[a-zA-Z0-9_][a-zA-Z0-9_-]*)*$`).MatchString(c.Path) {
			return fmt.Errorf("companion path must be a literal nested directory")
		}
		if c.Path == m.Components[0].Path || seen[c.Path] {
			return fmt.Errorf("duplicate component path in companion %d", i+1)
		}
		seen[c.Path] = true
		if !regexp.MustCompile(`^project:verify:[a-z][a-z0-9-]*$`).MatchString(c.VerifyTask) {
			return fmt.Errorf("invalid companion verification task")
		}
		if seenTasks[c.VerifyTask] {
			return fmt.Errorf("duplicate companion verification task in companion %d", i+1)
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
		return inputError(".standards/tasks.project.toml", source, err)
	}
	for i, c := range m.Companions {
		task, ok := tasks[c.VerifyTask].(map[string]any)
		if !ok {
			return fmt.Errorf("define the verification task of companion %d in .standards/tasks.project.toml", i+1)
		}
		// A success receipt must not be issued because mise skipped a companion.
		for _, key := range []string{"platforms", "sources", "outputs", "skip", "depends", "depends_post", "wait_for"} {
			if _, ok := task[key]; ok {
				return fmt.Errorf("the verification task of companion %d cannot use %s", i+1, key)
			}
		}
		switch run := task["run"].(type) {
		case string:
			if strings.TrimSpace(run) == "" {
				return fmt.Errorf("the verification task of companion %d is empty", i+1)
			}
		case []any:
			if len(run) == 0 {
				return fmt.Errorf("the verification task of companion %d is empty", i+1)
			}
			for _, command := range run {
				if s, ok := command.(string); !ok || strings.TrimSpace(s) == "" {
					return fmt.Errorf("invalid companion command")
				}
			}
		default:
			return fmt.Errorf("the verification task of companion %d needs an explicit run command", i+1)
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
