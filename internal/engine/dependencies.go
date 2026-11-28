package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

const (
	dependencyIntakeRule = "CORE-DEPENDENCY-INTAKE"
	lockfileSourceRule   = "CORE-LOCKFILE-SOURCE"
	// pnpm 11 で minimumReleaseAge の既定が 1 日になり、依存の build script も既定で止まる。
	pnpmSafeDefaultsMajor = 11
)

// A finding is one judgment about one file. Its text never quotes file
// content, because .npmrc can hold credentials.
type finding struct {
	status, path, detail string
}

// Project-level npm settings are read here instead of through the plan: the
// plan is printed and saved, and .npmrc can hold registry credentials.
func (t *Tree) dependencyResults(p *Plan, component string) []Result {
	inputs := map[string]Snapshot{}
	for name, s := range p.Inputs {
		inputs[name] = s
	}
	unreadable := map[string]error{}
	for name, s := range p.Inputs {
		if !s.Exists || path.Base(name) != "package-lock.json" {
			continue
		}
		npmrc := path.Join(path.Dir(name), ".npmrc")
		v, err := t.Read(npmrc)
		if err != nil {
			unreadable[npmrc] = err
			continue
		}
		inputs[npmrc] = v
	}
	return dependencyResults(inputs, unreadable, component)
}

func dependencyResults(inputs map[string]Snapshot, unreadable map[string]error, component string) []Result {
	names := make([]string, 0, len(inputs))
	for name, s := range inputs {
		if s.Exists {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var cooldown, scripts, sources []finding
	for _, name := range names {
		dir, content := path.Dir(name), inputs[name].Content
		switch path.Base(name) {
		case "package-lock.json":
			npmrc := path.Join(dir, ".npmrc")
			if err := unreadable[npmrc]; err != nil {
				f := finding{"warn", npmrc, "cannot be read: " + err.Error()}
				cooldown, scripts = append(cooldown, f), append(scripts, f)
			} else {
				settings := npmrcSettings(inputs[npmrc].Content)
				cooldown = append(cooldown, npmCooldown(npmrc, settings))
				scripts = append(scripts, npmScripts(npmrc, settings))
			}
			sources = append(sources, sourceFinding(name, "https://registry.npmjs.org/ with sha512 integrity", npmLockViolations, content))
		case "pnpm-lock.yaml":
			workspace := path.Join(dir, "pnpm-workspace.yaml")
			c, s := pnpmSettings(workspace, inputs[workspace].Content, pnpmMajor(inputs, dir))
			cooldown, scripts = append(cooldown, c), append(scripts, s)
			sources = append(sources, sourceFinding(name, "the registry with sha512 integrity", pnpmLockViolations, content))
		case "uv.lock":
			project := path.Join(dir, "pyproject.toml")
			cooldown = append(cooldown, uvCooldown(project, inputs[project].Content))
			sources = append(sources, sourceFinding(name, "https://pypi.org/simple with sha256 hashes", uvLockViolations, content))
		}
	}
	return []Result{
		summarize(dependencyIntakeRule, "cooldown", component, "package-lock.json, pnpm-lock.yaml or uv.lock", cooldown),
		// uv の sdist のビルドは判定しない。禁止すると wheel のない依存を導入できなくなる。
		summarize(dependencyIntakeRule, "install-scripts", component, "package-lock.json or pnpm-lock.yaml", scripts),
		summarize(lockfileSourceRule, "source", component, "package-lock.json, pnpm-lock.yaml or uv.lock", sources),
	}
}

func summarize(rule, check, component, subjects string, findings []finding) Result {
	if len(findings) == 0 {
		return Result{rule, check, component, "not_applicable", "no " + subjects + " among the plan inputs"}
	}
	rank := map[string]int{"pass": 0, "warn": 1, "fail": 2}
	status := "pass"
	parts := make([]string, len(findings))
	for i, f := range findings {
		if rank[f.status] > rank[status] {
			status = f.status
		}
		parts[i] = f.status + " " + f.path + ": " + f.detail
	}
	return Result{rule, check, component, status, strings.Join(parts, "; ")}
}

func sourceFinding(name, expected string, violations func(string) ([]string, error), content string) finding {
	found, err := violations(content)
	if err != nil {
		return finding{"fail", name, "cannot be parsed: " + err.Error()}
	}
	if len(found) == 0 {
		return finding{"pass", name, "every package comes from " + expected}
	}
	shown := found
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return finding{"fail", name, fmt.Sprintf("%d packages do not come from %s: %s", len(found), expected, strings.Join(shown, ", "))}
}

// npmrcSettings keeps the last value of each key, as npm does.
func npmrcSettings(content string) map[string]string {
	settings := map[string]string{}
	for _, line := range strings.Split(normalized(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		settings[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return settings
}

func npmCooldown(npmrc string, settings map[string]string) finding {
	days, err := strconv.ParseFloat(settings["min-release-age"], 64)
	if err != nil || days <= 0 {
		return finding{"warn", npmrc, "min-release-age is not set to a positive number of days"}
	}
	return finding{"pass", npmrc, "min-release-age=" + strconv.FormatFloat(days, 'f', -1, 64) + " days"}
}

func npmScripts(npmrc string, settings map[string]string) finding {
	if settings["ignore-scripts"] != "true" {
		return finding{"warn", npmrc, "ignore-scripts=true is not set"}
	}
	return finding{"pass", npmrc, "ignore-scripts=true"}
}

// pnpmMajor prefers packageManager, which pnpm itself enforces, over mise.
func pnpmMajor(inputs map[string]Snapshot, dir string) int {
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	_ = json.Unmarshal([]byte(inputs[path.Join(dir, "package.json")].Content), &pkg)
	version, ok := strings.CutPrefix(pkg.PackageManager, "pnpm@")
	if !ok {
		var mise struct {
			Tools map[string]any `toml:"tools"`
		}
		_, _ = toml.Decode(inputs[".standards/mise.project.toml"].Content, &mise)
		version, _ = mise.Tools["pnpm"].(string)
	}
	major, _, _ := strings.Cut(version, ".")
	n, _ := strconv.Atoi(major)
	return n
}

func pnpmSettings(workspace, content string, major int) (cooldown, scripts finding) {
	var cfg struct {
		MinimumReleaseAge         *float64       `yaml:"minimumReleaseAge"`
		DangerouslyAllowAllBuilds bool           `yaml:"dangerouslyAllowAllBuilds"`
		AllowBuilds               map[string]any `yaml:"allowBuilds"`
	}
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		f := finding{"warn", workspace, "cannot be parsed: " + err.Error()}
		return f, f
	}
	safeDefaults := major >= pnpmSafeDefaultsMajor
	version := "the pnpm version is not pinned in packageManager or mise"
	if major > 0 {
		version = fmt.Sprintf("pnpm %d", major)
	}
	switch {
	case cfg.MinimumReleaseAge != nil && *cfg.MinimumReleaseAge > 0:
		cooldown = finding{"pass", workspace, fmt.Sprintf("minimumReleaseAge=%s minutes", strconv.FormatFloat(*cfg.MinimumReleaseAge, 'f', -1, 64))}
	case cfg.MinimumReleaseAge != nil:
		cooldown = finding{"warn", workspace, "minimumReleaseAge disables the cooldown"}
	case safeDefaults:
		cooldown = finding{"pass", workspace, version + " applies its default minimumReleaseAge of 1440 minutes"}
	default:
		cooldown = finding{"warn", workspace, "minimumReleaseAge is not set and " + version + " has no default cooldown"}
	}
	switch {
	case cfg.DangerouslyAllowAllBuilds:
		scripts = finding{"warn", workspace, "dangerouslyAllowAllBuilds runs every dependency build script"}
	case safeDefaults:
		allowed := 0
		for _, v := range cfg.AllowBuilds {
			if v == true {
				allowed++
			}
		}
		scripts = finding{"pass", workspace, fmt.Sprintf("%s blocks dependency build scripts unless allowBuilds permits them (%d permitted)", version, allowed)}
	default:
		scripts = finding{"warn", workspace, "allowBuilds needs pnpm 11 or later, and " + version}
	}
	return cooldown, scripts
}

func uvCooldown(project, content string) finding {
	var cfg struct {
		Tool struct {
			UV struct {
				ExcludeNewer any `toml:"exclude-newer"`
			} `toml:"uv"`
		} `toml:"tool"`
	}
	if _, err := toml.Decode(content, &cfg); err != nil {
		return finding{"warn", project, "cannot be parsed: " + err.Error()}
	}
	if s, ok := cfg.Tool.UV.ExcludeNewer.(string); ok && strings.TrimSpace(s) != "" {
		return finding{"pass", project, fmt.Sprintf("tool.uv.exclude-newer=%q", s)}
	}
	return finding{"warn", project, "tool.uv.exclude-newer is not set"}
}

// pnpm 12 can write a boot document before the project's dependency graph, so
// every document is checked instead of the first one.
func pnpmLockViolations(content string) ([]string, error) {
	var found []string
	dec := yaml.NewDecoder(strings.NewReader(content))
	for {
		var doc struct {
			Packages map[string]struct {
				Resolution map[string]any `yaml:"resolution"`
			} `yaml:"packages"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		for name, pkg := range doc.Packages {
			r := pkg.Resolution
			if r["type"] == "directory" {
				continue
			}
			tarball, hasTarball := r["tarball"].(string)
			integrity, _ := r["integrity"].(string)
			_, git := r["repo"]
			if git || r["type"] == "git" || (hasTarball && !strings.HasPrefix(tarball, "https://registry.npmjs.org/")) || !strings.HasPrefix(integrity, "sha512-") {
				found = append(found, name)
			}
		}
	}
	sort.Strings(found)
	return found, nil
}
