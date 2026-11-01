package engine

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	bundle "github.com/tqer39/dev-standards"
)

const decisionTarget = "docs/decisions/0001-language.md"
const vendorPath = ".standards/vendor/gitignore/go-linux-macos-windows.txt"
const ignoreURL = "https://www.toptal.com/developers/gitignore/api/go,linux,macos,windows"

var managedPaths = []string{".editorconfig", ".gitignore", ".gitattributes", "mise.toml", ".standards/tasks.standards.toml"}
var reservedTasks = []string{"bootstrap", "lint", "format", "format:check", "test", "build", "check", "standards:plan", "standards:check", "standards:verify", "standards:prepare", "standards:finish"}

func (t *Tree) Plan(req Request) (*Plan, error) {
	if _, err := bundle.ResolveProfile("go"); err != nil {
		return nil, err
	}
	p := &Plan{SchemaVersion: 1, Version: bundle.Version, Bundle: bundle.Digest(), Root: t.Root, HEAD: t.head(), Request: req, Inputs: map[string]Snapshot{}, Changes: []Change{}, Conflicts: []string{}}
	read := func(name string) (Snapshot, error) {
		v, err := t.Read(name)
		if err == nil {
			p.Inputs[name] = v
		}
		return v, err
	}
	names := []string{".standards/repository.yml", ".standards/lock.json", ".standards/state.json", ".standards/exceptions.yml", ".standards/editorconfig.local", ".standards/gitattributes.local", ".standards/gitignore.local", ".standards/mise.project.toml", ".standards/tasks.project.toml", vendorPath, decisionTarget, "go.mod", "go.sum"}
	names = append(names, managedPaths...)
	if req.DecisionPath != "" {
		if err := validateInputPath(req.DecisionPath); err != nil {
			return nil, err
		}
		names = append(names, req.DecisionPath)
	}
	for _, name := range names {
		if _, err := read(name); err != nil {
			return nil, err
		}
	}
	initial := req.Mode == "adopt"
	upgrading := req.Mode == "upgrade"
	if err := t.validateTaskDirectories(); err != nil {
		return nil, err
	}
	if req.Mode != "adopt" && req.Mode != "reapply" && !upgrading {
		return nil, fmt.Errorf("unknown plan mode")
	}
	var m Manifest
	if initial {
		if req.Profile != "go" && req.Profile != "python-uv" && req.Profile != "node-npm" {
			return nil, fmt.Errorf("supported profiles: go, python-uv, node-npm")
		}
		if req.Instructions != "" && req.Instructions != "rulesync" {
			return nil, fmt.Errorf("supported instruction generators: rulesync")
		}
		if p.Inputs[".standards/repository.yml"].Exists || p.Inputs[".standards/state.json"].Exists || p.Inputs[".standards/lock.json"].Exists || p.Inputs[".standards/tasks.standards.toml"].Exists {
			return nil, fmt.Errorf("repository already adopted; use plan")
		}
		m = defaultManifest()
		if req.ComponentPath != "" {
			m.Components[0].Path = req.ComponentPath
		}
		m.Components[0].Profile = req.Profile
		m.Gitignore.Templates[0] = profileLanguage(req.Profile)
		m.AI.Instructions = req.Instructions
	} else {
		if req.Profile != "" || req.DecisionPath != "" || req.ComponentPath != "" || req.Instructions != "" {
			return nil, fmt.Errorf("reapply does not accept adoption options")
		}
		if err := bundle.Decode([]byte(p.Inputs[".standards/repository.yml"].Content), "repository", &m); err != nil {
			return nil, fmt.Errorf("repository.yml: %w", err)
		}
	}
	previousAutomation := len(m.AI.Targets) > 0
	if upgrading {
		if !nonDowngrade(m.Standard.Version, bundle.Version) {
			return nil, fmt.Errorf("automatic downgrades are forbidden")
		}
		previousVersion, _ := versionParts(m.Standard.Version)
		currentVersion, _ := versionParts(bundle.Version)
		if !m.Update.AllowMajor && previousVersion[0] != currentVersion[0] {
			return nil, fmt.Errorf("major upgrades are disabled")
		}
		m.Standard.Version = bundle.Version
	}
	if req.Automation {
		m.AI.Targets = []string{"codexcli"}
		m.Update.Rollout = "standard"
	}
	managedPaths := append([]string{}, managedPaths...)
	if len(m.AI.Targets) > 0 {
		managedPaths = append(managedPaths, automationPathsFor(m)...)
	}
	for _, name := range automationInputs {
		if _, err := read(name); err != nil {
			return nil, err
		}
	}
	if err := validateManifest(m); err != nil {
		return nil, err
	}
	component := m.Components[0]
	if err := t.validateLayoutFor(component); err != nil {
		return nil, err
	}
	vendorPath, ignoreURL := vendorPath, ignoreURL
	language := profileLanguage(component.Profile)
	if component.Profile != "go" {
		vendorPath = ".standards/vendor/gitignore/" + language + "-linux-macos-windows.txt"
		ignoreURL = "https://www.toptal.com/developers/gitignore/api/" + language + ",linux,macos,windows"
		if _, err := read(vendorPath); err != nil {
			return nil, err
		}
		for _, name := range componentInputs(component) {
			if _, err := read(name); err != nil {
				return nil, err
			}
		}
	}
	if component.Profile == "python-uv" {
		if err := validatePythonInputs(p, component); err != nil {
			return nil, err
		}
	} else if component.Profile == "node-npm" {
		if err := validateNodeInputs(p, component); err != nil {
			return nil, err
		}
	} else if !p.Inputs["go.mod"].Exists {
		return nil, fmt.Errorf("a Go module is required before adoption")
	}
	desired := map[string]Snapshot{}
	seed := func(name, content string) {
		if !p.Inputs[name].Exists {
			desired[name] = file(content)
		}
	}
	if initial {
		seed(".standards/repository.yml", string(JSON(m)))
	} else if upgrading || req.Automation {
		desired[".standards/repository.yml"] = file(string(JSON(m)))
	}
	if !p.Inputs[".standards/exceptions.yml"].Exists {
		seed(".standards/exceptions.yml", "schemaVersion: 1\nexceptions: []\n")
	}
	exception := p.Inputs[".standards/exceptions.yml"].Content
	if !p.Inputs[".standards/exceptions.yml"].Exists {
		exception = "schemaVersion: 1\nexceptions: []\n"
	}
	var ex struct {
		Exceptions []any `json:"exceptions"`
	}
	if err := bundle.Decode([]byte(exception), "exceptions", &ex); err != nil {
		return nil, fmt.Errorf("exceptions.yml: %w", err)
	}
	if len(ex.Exceptions) > 0 {
		return nil, fmt.Errorf("basic rules are non-waivable; exceptions must be empty")
	}
	decision := p.Inputs[decisionTarget].Content
	if !p.Inputs[decisionTarget].Exists && initial && req.DecisionPath != "" {
		decision = p.Inputs[req.DecisionPath].Content
		seed(decisionTarget, decision)
	}
	if err := languageDecisionFor(decision, component.Profile); err != nil {
		return nil, err
	}

	editorLocal := p.Inputs[".standards/editorconfig.local"].Content
	ignoreLocal := p.Inputs[".standards/gitignore.local"].Content
	miseLocal := p.Inputs[".standards/mise.project.toml"].Content
	attributesLocal := p.Inputs[".standards/gitattributes.local"].Content
	importedModes := map[string]uint32{}
	if initial {
		for _, entry := range []struct {
			target, local string
			value         *string
		}{{".editorconfig", ".standards/editorconfig.local", &editorLocal}, {".gitignore", ".standards/gitignore.local", &ignoreLocal}, {"mise.toml", ".standards/mise.project.toml", &miseLocal}, {".gitattributes", ".standards/gitattributes.local", &attributesLocal}} {
			old := p.Inputs[entry.target]
			if old.Exists {
				if p.Inputs[entry.local].Exists {
					return nil, fmt.Errorf("both %s and %s exist; resolve their ownership before adoption", entry.target, entry.local)
				}
				*entry.value = old.Content
				importedModes[entry.local] = old.Mode
			}
		}
	}
	localEditor, err := cleanLocalEditorFor(editorLocal, component.Profile)
	if err != nil {
		return nil, err
	}
	seed(".standards/editorconfig.local", localEditor)
	seed(".standards/gitignore.local", ignoreLocal)
	seed(".standards/mise.project.toml", normalized(miseLocal))
	seed(".standards/gitattributes.local", normalized(attributesLocal))
	seed(".standards/tasks.project.toml", "# リポジトリ固有のタスク。共通の予約名は定義しない。\n")
	if err := validateProjectTasks(p.Inputs[".standards/tasks.project.toml"].Content); err != nil {
		return nil, err
	}
	rootMise, err := renderMiseFor(miseLocal, component.Profile)
	if err != nil {
		return nil, err
	}
	snapshot := bundle.Read("templates/base/gitignore.txt")
	if component.Profile != "go" {
		snapshot = bundle.Read("templates/base/gitignore-" + language + ".txt")
	}
	if upgrading {
		var previous Lock
		if err := strictJSON(p.Inputs[".standards/lock.json"].Content, &previous); err != nil {
			return nil, err
		}
		if sum(p.Inputs[vendorPath].Content) != previous.GitignoreSHA256 {
			return nil, fmt.Errorf("previous gitignore snapshot is not authentic")
		}
		desired[vendorPath] = file(snapshot)
	}
	if !upgrading && p.Inputs[vendorPath].Exists && p.Inputs[vendorPath].Content != snapshot {
		return nil, fmt.Errorf("existing gitignore snapshot conflicts with the selected bundle")
	}
	if initial {
		seed(vendorPath, snapshot)
	} else if !upgrading && p.Inputs[vendorPath].Content != snapshot {
		return nil, fmt.Errorf("gitignore snapshot does not match the locked bundle")
	}
	var parsedMise struct {
		Tools map[string]string `toml:"tools"`
	}
	if _, err := toml.Decode(rootMise, &parsedMise); err != nil {
		return nil, err
	}
	lock := Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: bundle.Version, Bundle: bundle.Digest(), GitignoreSHA256: sum(snapshot), GitignoreURL: ignoreURL, GoVersion: parsedMise.Tools["go"]}
	if component.Profile == "python-uv" {
		lock.PythonVersion = parsedMise.Tools["python"]
		lock.UVVersion = parsedMise.Tools["uv"]
	}
	if component.Profile == "node-npm" {
		lock.NodeVersion = parsedMise.Tools["node"]
	}
	if !initial {
		var current Lock
		if err := strictJSON(p.Inputs[".standards/lock.json"].Content, &current); err != nil {
			return nil, fmt.Errorf("lock.json: %w", err)
		}
		if upgrading && (current.Repository != bundle.Repository || current.SchemaVersion != 1 || !nonDowngrade(current.Version, bundle.Version)) {
			return nil, fmt.Errorf("untrusted previous lock")
		}
		if current != lock && !upgrading {
			return nil, fmt.Errorf("lock does not match this CLI bundle; implicit updates are forbidden")
		}
	} else {
		seed(".standards/lock.json", string(JSON(lock)))
	}
	if upgrading {
		desired[".standards/lock.json"] = file(string(JSON(lock)))
	}
	desired[".editorconfig"] = file(bundle.Read("templates/base/editorconfig.txt") + "\n# リポジトリ固有の設定\n" + localEditor)
	desired[".gitignore"] = file(snapshot + "\n# dev-standards\n/.standards/cache/\n/.standards/plans/\n/.dev-standards-apply.lock\n/bin/\n.env\n.env.*\n!.env.example\n\n# リポジトリ固有の設定\n" + ignoreLocal)
	desired[".gitattributes"] = file(normalized(attributesLocal) + "\n# dev-standards: deterministic checkout\n*.go text eol=lf\ngo.mod text eol=lf\ngo.sum text eol=lf\n/.editorconfig text eol=lf\n/mise.toml text eol=lf\n/.gitattributes text eol=lf\n/AGENTS.md text eol=lf\n/.codex/hooks.json text eol=lf\n/.github/workflows/dev-standards.yml text eol=lf\n/.gitignore -text whitespace=-blank-at-eol\n/.standards/** text eol=lf\n/.standards/gitignore.local -text whitespace=-blank-at-eol\n/.standards/vendor/gitignore/*.txt -text whitespace=-blank-at-eol\n/docs/decisions/0001-language.md text eol=lf\n")
	if component.Profile == "python-uv" {
		desired[".editorconfig"] = file(bundle.Read("templates/base/editorconfig.txt") + "\n[*.py]\nindent_size = 4\n\n# リポジトリ固有の設定\n" + localEditor)
		attrs := desired[".gitattributes"]
		attrs.Content = strings.Replace(attrs.Content, "*.go text eol=lf\ngo.mod text eol=lf\ngo.sum text eol=lf", "*.py text eol=lf\n"+componentFile(component, "pyproject.toml")+" text eol=lf\n"+componentFile(component, "uv.lock")+" text eol=lf", 1)
		desired[".gitattributes"] = attrs
	}
	if component.Profile == "node-npm" {
		attrs := desired[".gitattributes"]
		attrs.Content = strings.Replace(attrs.Content, "*.go text eol=lf\ngo.mod text eol=lf\ngo.sum text eol=lf", "*.js text eol=lf\n*.mjs text eol=lf\n*.cjs text eol=lf\n*.ts text eol=lf\n*.tsx text eol=lf\n"+componentFile(component, "package.json")+" text eol=lf\n"+componentFile(component, "package-lock.json")+" text eol=lf", 1)
		desired[".gitattributes"] = attrs
	}
	if m.AI.Instructions == "rulesync" && len(m.AI.Targets) > 0 {
		attrs := desired[".gitattributes"]
		attrs.Content += "/" + rulesyncEntryPath + " text eol=lf\n"
		desired[".gitattributes"] = attrs
	}
	desired["mise.toml"] = file(rootMise)
	desired[".standards/tasks.standards.toml"] = file(componentTasks(component, p.Inputs[".standards/tasks.project.toml"].Content))
	if len(m.AI.Targets) > 0 {
		if err := t.automationFiles(p, desired, m); err != nil {
			return nil, err
		}
		desired[".standards/tasks.standards.toml"] = file(automatedTasksFor(componentTasks(component, p.Inputs[".standards/tasks.project.toml"].Content)))
	}
	required := append([]string{vendorPath}, componentInputs(component)...)
	if len(m.AI.Targets) > 0 {
		required = append(required, automationPathsFor(m)...)
	}
	if err := validateIgnore(desired[".gitignore"].Content, required...); err != nil {
		return nil, err
	}
	var state State
	if !initial {
		if err := strictJSON(p.Inputs[".standards/state.json"].Content, &state); err != nil {
			return nil, err
		}
		var previousLock Lock
		if err := strictJSON(p.Inputs[".standards/lock.json"].Content, &previousLock); err != nil {
			return nil, err
		}
		if state.Bundle != previousLock.Bundle {
			return nil, fmt.Errorf("previous state and lock do not match")
		}
		if state.SchemaVersion != 1 || (!upgrading && state.Bundle != bundle.Digest()) || len(state.Files) < 5 {
			return nil, fmt.Errorf("state has an unsupported version or ownership set")
		}
		requiredOwnership := append([]string{}, []string{".editorconfig", ".gitignore", ".gitattributes", "mise.toml", ".standards/tasks.standards.toml"}...)
		if previousAutomation {
			requiredOwnership = append(requiredOwnership, automationPathsFor(m)...)
		}
		for _, name := range requiredOwnership {
			if _, ok := state.Files[name]; !ok {
				return nil, fmt.Errorf("state is missing required ownership: %s", name)
			}
		}
		for name, baseline := range state.Files {
			if !contains(managedPaths, name) {
				return nil, fmt.Errorf("unknown managed path in previous state: %s", name)
			}
			ok := baseline.Exists
			if !ok {
				return nil, fmt.Errorf("state is missing %s", name)
			}
			current := p.Inputs[name]
			if _, exists := p.Inputs[name]; !exists {
				if _, err := read(name); err != nil {
					return nil, err
				}
				current = p.Inputs[name]
			}
			if !equal(current, baseline) && !t.committedDeletion(name, current) {
				p.Conflicts = append(p.Conflicts, "managed file changed: "+name+"; move changes to its .standards source before reapplying")
			}
		}
	}
	for _, name := range managedPaths {
		if _, exists := p.Inputs[name]; !exists {
			if _, err := read(name); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range managedPaths {
		if p.Inputs[name].Exists {
			v := desired[name]
			v.Mode = p.Inputs[name].Mode
			desired[name] = v
		}
	}
	for name, mode := range importedModes {
		if v, ok := desired[name]; ok {
			v.Mode = mode
			desired[name] = v
		}
	}
	state = State{1, bundle.Digest(), map[string]Snapshot{}}
	for _, name := range managedPaths {
		state.Files[name] = desired[name]
	}
	stateFile := file(string(JSON(state)))
	for _, v := range state.Files {
		stateFile.Mode &= v.Mode
	}
	desired[".standards/state.json"] = stateFile
	paths := make([]string, 0, len(desired))
	for name := range desired {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		before, ok := p.Inputs[name]
		if !ok {
			var err error
			before, err = read(name)
			if err != nil {
				return nil, err
			}
		}
		after := desired[name]
		if len(after.Content) > maxFile {
			return nil, fmt.Errorf("generated file exceeds 1 MiB: %s", name)
		}
		if before.Exists {
			if name == ".standards/state.json" {
				after.Mode &= before.Mode
			} else {
				after.Mode = before.Mode
			}
			desired[name] = after
		}
		if !equal(before, after) {
			p.Changes = append(p.Changes, Change{name, before, after})
		}
	}
	sort.Strings(p.Conflicts)
	return p, nil
}

func validateProjectTasks(s string) error {
	var tasks map[string]any
	if _, err := toml.Decode(s, &tasks); err != nil {
		return fmt.Errorf("project tasks: %w", err)
	}
	for _, name := range reservedTasks {
		if _, ok := tasks[name]; ok {
			return fmt.Errorf("reserved task in tasks.project.toml: %s", name)
		}
	}
	return nil
}
func renderMiseFor(local, profileID string) (string, error) {
	local = normalized(local)
	var cfg map[string]any
	meta, err := toml.Decode(local, &cfg)
	if err != nil {
		return "", fmt.Errorf("mise project configuration: %w", err)
	}
	if meta.IsDefined("task_config") {
		return "", fmt.Errorf("existing task_config needs manual migration; its include order will not be overwritten")
	}
	for _, name := range reservedTasks {
		if meta.IsDefined("tasks", name) {
			return "", fmt.Errorf("reserved task %s already exists; migrate its behavior explicitly", name)
		}
	}
	s := local
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if profileID == "python-uv" {
		if err := validatePythonTools(cfg); err != nil {
			return "", err
		}
	} else if profileID == "node-npm" {
		if err := validateNodeTools(cfg); err != nil {
			return "", err
		}
	} else if !meta.IsDefined("tools", "go") {
		if meta.IsDefined("tools") {
			return "", fmt.Errorf("add an explicit pinned tools.go to the existing tools table before adoption")
		}
		profile, err := bundle.ResolveProfile("go")
		if err != nil {
			return "", err
		}
		s += fmt.Sprintf("\n[tools]\ngo = %q\n", profile.Tools["go"])
	} else {
		tools, ok := cfg["tools"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("tools must be a table")
		}
		version, ok := tools["go"].(string)
		if !ok || !pinnedGo(version) {
			return "", fmt.Errorf("tools.go must be an exact x.y.z version")
		}
	}
	s += "\n# Generated by dev-standards; edit .standards/mise.project.toml.\n[task_config]\nincludes = [\"mise-tasks\", \".mise/tasks\", \".config/mise/tasks\", \".standards/tasks.project.toml\", \".standards/tasks.standards.toml\"]\n"
	return s, nil
}
func pinnedGo(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
func tasks() string {
	return `[bootstrap]
run = "go mod download"

[lint]
run = "go vet ./..."

[format]
run = "go fmt ./..."

["format:check"]
run = "dev-standards format-check"

[test]
run = "go test ./..."

[build]
run = "go build -o bin/ ./..."

[check]
depends = ["standards:check", "lint", "format:check", "test"]

["standards:check"]
run = "dev-standards check"

["standards:plan"]
run = "dev-standards plan"

["standards:verify"]
run = "dev-standards verify"
`
}

func validateIgnore(content string, required ...string) error {
	dir, err := os.MkdirTemp("", "dev-standards-ignore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0600); err != nil {
		return err
	}
	if out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %s: %w", out, err)
	}
	check := func(paths string) (string, error) {
		cmd := exec.Command("git", "-c", "core.excludesFile="+os.DevNull, "-C", dir, "check-ignore", "--no-index", "--stdin")
		cmd.Stdin = strings.NewReader(paths)
		out, err := cmd.Output()
		if e, ok := err.(*exec.ExitError); ok && e.ExitCode() == 1 {
			return "", nil
		}
		return string(out), err
	}
	tracked := "AGENTS.md\n.codex/hooks.json\n.github/workflows/dev-standards.yml\n.standards/rules.md\n.standards/agents.local.md\n.standards/hooks.local.json\n.gitattributes\n.standards/gitattributes.local\n.standards/repository.yml\n.standards/lock.json\n.standards/state.json\n.standards/exceptions.yml\n.standards/gitignore.local\n.standards/editorconfig.local\n.standards/mise.project.toml\n.standards/tasks.project.toml\n.standards/tasks.standards.toml\n" + vendorPath + "\n.editorconfig\n.gitignore\nmise.toml\ngo.mod\ngo.sum\n" + decisionTarget + "\n"
	tracked += strings.Join(required, "\n") + "\n"
	ignored, err := check(tracked)
	if err != nil {
		return err
	}
	if ignored != "" {
		return fmt.Errorf("gitignore excludes required tracked files: %s", strings.TrimSpace(ignored))
	}
	ignored, err = check(".env\n.env.local\n.standards/plans/plan.json\n.standards/cache/dev-standards\n.dev-standards-apply.lock\n")
	if err != nil {
		return err
	}
	if len(strings.Fields(ignored)) != 5 {
		return fmt.Errorf("local gitignore negates required private/temporary exclusions")
	}
	return nil
}

func samePlan(a, b *Plan) bool { return reflect.DeepEqual(a, b) }

// Existing external task directories need an explicit migration because their
// discovery and precedence cannot be preserved by a generic text rewrite.
func (t *Tree) validateTaskDirectories() error {
	for _, dir := range []string{"mise-tasks", ".mise/tasks", ".config/mise/tasks"} {
		err := fs.WalkDir(t.fs.FS(), dir, func(p string, d fs.DirEntry, e error) error {
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if err := t.safe(p); err != nil {
				return err
			}
			if !d.IsDir() {
				return fmt.Errorf("existing task directory %s needs manual migration", dir)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func (t *Tree) committedDeletion(name string, current Snapshot) bool {
	if current.Exists || t.head() == "unborn" {
		return false
	}
	cmd := exec.Command("git", "-C", t.Root, "status", "--porcelain=v1", "--", name)
	b, err := cmd.Output()
	return err == nil && len(b) == 0
}

func (t *Tree) validateLayoutFor(component Component) error {
	return fs.WalkDir(t.fs.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		switch d.Name() {
		case "go.mod":
			if p != "go.mod" || component.Profile != "go" {
				return fmt.Errorf("nested Go module needs a separate profile: %s", p)
			}
		case "pyproject.toml":
			if component.Profile != "python-uv" || p != componentFile(component, "pyproject.toml") {
				return fmt.Errorf("additional Python component is unsupported: %s", p)
			}
		case "package.json":
			if component.Profile != "node-npm" || p != componentFile(component, "package.json") {
				return fmt.Errorf("additional Node package is unsupported: %s", p)
			}
		case "Cargo.toml", "Package.swift":
			return fmt.Errorf("additional language needs a separate profile: %s", p)
		}
		return nil
	})
}
