package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

const (
	dependencyIntakeRule = "CORE-DEPENDENCY-INTAKE"
	lockfileSourceRule   = "CORE-LOCKFILE-SOURCE"
	// pnpm 11 で minimumReleaseAge の既定が 1 日になり、依存の build script も既定で止まる。
	pnpmSafeDefaultsMajor = 11
	npmRegistry           = "https://registry.npmjs.org/"
)

// A finding is one judgment about one file. Its text is built from fixed
// phrases and values this package formats itself: configuration files and
// lockfile keys can hold credentials, and the report and the error reach
// stdout, stderr and CI logs.
type finding struct {
	status, path, detail string
}

// dependencyContext carries what the judgments need beyond the plan inputs.
type dependencyContext struct {
	// inputs holds the plan inputs plus the settings read only for the check.
	inputs map[string]Snapshot
	// unreadable lists settings that exist but cannot be read safely.
	unreadable map[string]bool
	// localDir reports whether a repository-relative path is a real directory
	// inside the repository, reached without following a symlink.
	localDir func(string) bool
	now      time.Time
}

// .npmrc and uv.toml are read here instead of through the plan: the plan is
// printed and saved, and both files can hold registry credentials.
func (t *Tree) dependencyResults(p *Plan, component string) []Result {
	ctx := dependencyContext{inputs: map[string]Snapshot{}, unreadable: map[string]bool{}, localDir: t.localDirectory, now: time.Now()}
	for name, s := range p.Inputs {
		ctx.inputs[name] = s
	}
	for name, s := range p.Inputs {
		if !s.Exists {
			continue
		}
		var settings []string
		switch path.Base(name) {
		case "package-lock.json":
			settings = []string{".npmrc"}
		case "pnpm-lock.yaml":
			settings = []string{".npmrc", "pnpm-workspace.yaml"}
		case "uv.lock":
			settings = []string{"uv.toml"}
		}
		for _, base := range settings {
			file := path.Join(path.Dir(name), base)
			if _, planned := p.Inputs[file]; planned {
				continue
			}
			v, err := t.Read(file)
			if err != nil {
				ctx.unreadable[file] = true
				continue
			}
			ctx.inputs[file] = v
		}
	}
	return dependencyResults(ctx, component)
}

func (t *Tree) localDirectory(p string) bool {
	if p == "." {
		return true
	}
	if t.safe(p) != nil {
		return false
	}
	info, err := t.fs.Lstat(p)
	return err == nil && info.IsDir()
}

func dependencyResults(ctx dependencyContext, component string) []Result {
	names := make([]string, 0, len(ctx.inputs))
	for name, s := range ctx.inputs {
		if s.Exists {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var cooldown, scripts, sources []finding
	for _, name := range names {
		dir, content := path.Dir(name), ctx.inputs[name].Content
		local := func(rel string) bool {
			p, ok := repositoryPath(dir, rel)
			return ok && ctx.localDir != nil && ctx.localDir(p)
		}
		npmrc := path.Join(dir, ".npmrc")
		settings, opaque := npmrcSettings(ctx.inputs[npmrc].Content)
		switch path.Base(name) {
		case "package-lock.json":
			if ctx.unreadable[npmrc] {
				f := finding{"warn", npmrc, "cannot be read safely"}
				cooldown, scripts = append(cooldown, f), append(scripts, f)
			} else {
				cooldown = append(cooldown, npmCooldown(npmrc, settings, npmVersion(ctx.inputs)))
				scripts = append(scripts, npmScripts(npmrc, settings))
			}
			found, err := npmLockViolations(content, local)
			sources = append(sources, sourceFinding(name, npmRegistry+" with sha512 integrity, and the project settings keep that registry", npmPackageKey, found, err, npmrcRegistryProblems(npmrc, settings, opaque, ctx.unreadable[npmrc])))
		case "pnpm-lock.yaml":
			workspace := path.Join(dir, "pnpm-workspace.yaml")
			cfg, ok := pnpmWorkspace(ctx.inputs[workspace].Content)
			if ctx.unreadable[workspace] {
				ok = false
			}
			c, s := pnpmSettings(workspace, cfg, ok, pnpmMajor(ctx.inputs, dir))
			cooldown, scripts = append(cooldown, c), append(scripts, s)
			problems := append(npmrcRegistryProblems(npmrc, settings, opaque, ctx.unreadable[npmrc]), pnpmRegistryProblems(workspace, cfg, ok)...)
			found, err := pnpmLockViolations(content, local)
			sources = append(sources, sourceFinding(name, npmRegistry+" with sha512 integrity, and the project settings keep that registry", pnpmPackageKey, found, err, problems))
		case "uv.lock":
			cooldown = append(cooldown, uvCooldown(ctx, dir))
			found, err := uvLockViolations(content, local)
			sources = append(sources, sourceFinding(name, "https://pypi.org/simple with sha256 hashes", uvPackageName, found, err, nil))
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

// repositoryPath resolves a lockfile's local reference against the lockfile's
// directory. Absolute paths, URLs and paths that leave the repository are not
// local to it.
func repositoryPath(dir, rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, ":\\\x00") {
		return "", false
	}
	p := path.Clean(path.Join(dir, rel))
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

// A lockfile key is quoted only when it has the shape of a registry package:
// pnpm keys a URL dependency by its URL, which can include credentials, and
// any key can be set to an arbitrary string by whoever edits the lockfile.
var (
	npmPackageKey  = regexp.MustCompile(`^(?:[a-z0-9@._~-]+/)*node_modules/(?:@[a-z0-9._~-]+/)?[a-z0-9._~-]+$`)
	pnpmPackageKey = regexp.MustCompile(`^(?:@[a-z0-9._~-]+/)?[a-z0-9._~-]+@[0-9][A-Za-z0-9._+-]*$`)
	uvPackageName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

func safeNames(names []string, printable *regexp.Regexp) []string {
	sort.Strings(names)
	safe := make([]string, len(names))
	for i, name := range names {
		safe[i] = name
		if len(name) > 120 || !printable.MatchString(name) {
			safe[i] = fmt.Sprintf("(entry %d, name withheld)", i+1)
		}
	}
	return safe
}

func sourceFinding(name, expected string, printable *regexp.Regexp, found []string, err error, problems []string) finding {
	if err != nil {
		// パーサーのエラーは入力の値を引用することがあるので、載せない。
		return finding{"fail", name, "cannot be parsed"}
	}
	var parts []string
	if len(found) > 0 {
		shown := safeNames(found, printable)
		if len(shown) > 5 {
			shown = shown[:5]
		}
		parts = append(parts, fmt.Sprintf("%d packages are not fetched as required: %s", len(found), strings.Join(shown, ", ")))
	}
	parts = append(parts, problems...)
	if len(parts) == 0 {
		return finding{"pass", name, "every package comes from " + expected}
	}
	return finding{"fail", name, strings.Join(parts, "; ")}
}

var (
	npmrcSection    = regexp.MustCompile(`^\[([^\]]*)\]\s*$`)
	scopedRegistry  = regexp.MustCompile(`^@[^:]+:registry$`)
	npmrcLineBreaks = regexp.MustCompile(`[\r\n]+`)
)

// iniUnsafe decodes one key or value the way npm's ini parser does: a quoted
// token is read as JSON, and elsewhere a backslash escapes ; and #, which
// otherwise start a comment. exact is false when npm would not get a string.
func iniUnsafe(raw string) (value string, exact bool) {
	raw = strings.TrimSpace(raw)
	double := len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"'
	single := len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\''
	if double || single || raw == `"` || raw == `'` {
		if single {
			raw = raw[1 : len(raw)-1]
		}
		var decoded any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return raw, true
		}
		value, exact = decoded.(string)
		return value, exact
	}
	var b strings.Builder
	escaped := false
	for _, c := range raw {
		if escaped {
			if c != '\\' && c != ';' && c != '#' {
				b.WriteByte('\\')
			}
			b.WriteRune(c)
			escaped = false
		} else if c == ';' || c == '#' {
			break
		} else if c == '\\' {
			escaped = true
		} else {
			b.WriteRune(c)
		}
	}
	if escaped {
		b.WriteByte('\\')
	}
	return strings.TrimSpace(b.String()), true
}

// npmrcSettings reads a project .npmrc as npm's ini parser does and keeps the
// last value of each key. opaque reports a line that may set a registry in a
// way this function does not reproduce exactly: quoting that yields no string,
// a section, an array, a key npm expands from the environment, or an unknown
// key that mentions a registry. Such a file is never taken as keeping the
// official registry.
func npmrcSettings(content string) (settings map[string]string, opaque bool) {
	settings = map[string]string{}
	section := false
	for _, line := range npmrcLineBreaks.Split(content, -1) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#' {
			continue
		}
		if npmrcSection.MatchString(line) {
			section = true
			continue
		}
		rawKey, rawValue, hasValue := strings.Cut(line, "=")
		if rawKey == "" {
			continue
		}
		key, exact := iniUnsafe(rawKey)
		value := "true"
		if hasValue {
			var exactValue bool
			value, exactValue = iniUnsafe(rawValue)
			exact = exact && exactValue
		}
		array := len(key) > 2 && strings.HasSuffix(key, "[]")
		key = strings.TrimSuffix(key, "[]")
		registry := key == "registry" || scopedRegistry.MatchString(key)
		// 認証情報のキー（//host/:_authToken など）は、取得先を変えない。
		mentions := strings.Contains(strings.ToLower(key), "registry") && !strings.HasPrefix(key, "//")
		if !exact || strings.Contains(key, "${") || (mentions && (!registry || section || array)) {
			opaque = true
			continue
		}
		if !section && !array {
			settings[key] = value
		}
	}
	return settings, opaque
}

func officialNpmRegistry(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSuffix(s, "/") == strings.TrimSuffix(npmRegistry, "/")
}

// npm replaces the registry.npmjs.org host of a lockfile URL with the
// configured registry, and pnpm resolves integrity-only entries against it, so
// a project .npmrc can move every download without touching the lockfile.
func npmrcRegistryProblems(npmrc string, settings map[string]string, opaque, unreadable bool) []string {
	if unreadable {
		return []string{npmrc + " cannot be read safely, so the registry in effect is unknown"}
	}
	var problems []string
	if opaque {
		problems = append(problems, npmrc+" has a line that may set a registry and that this check cannot interpret as npm does")
	}
	scoped := false
	for key, value := range settings {
		if key == "registry" && !officialNpmRegistry(value) {
			problems = append(problems, npmrc+" sets registry to something other than "+npmRegistry)
		}
		if strings.HasSuffix(key, ":registry") && !officialNpmRegistry(value) {
			scoped = true
		}
	}
	if scoped {
		problems = append(problems, npmrc+" sets a scoped registry to something other than "+npmRegistry)
	}
	return problems
}

func pnpmWorkspace(content string) (map[string]any, bool) {
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, false
	}
	return cfg, true
}

func pnpmRegistryProblems(workspace string, cfg map[string]any, parsed bool) []string {
	if !parsed {
		return []string{workspace + " cannot be read or parsed, so the registry in effect is unknown"}
	}
	var problems []string
	if v, set := cfg["registry"]; set && !officialNpmRegistry(v) {
		problems = append(problems, workspace+" sets registry to something other than "+npmRegistry)
	}
	if v, set := cfg["registries"]; set && v != nil {
		ok := false
		if registries, isMap := v.(map[string]any); isMap {
			ok = true
			for key, value := range registries {
				// 新しい形は URL がキー、古い形は default か scope がキーで URL が値になる。
				_, declaration := value.(map[string]any)
				if !(declaration && officialNpmRegistry(key)) && !officialNpmRegistry(value) {
					ok = false
				}
			}
		}
		if !ok {
			problems = append(problems, workspace+" declares a registry other than "+npmRegistry+" in registries")
		}
	}
	for _, key := range []string{"namedRegistries", "pnprServer"} {
		if v, set := cfg[key]; set && v != nil {
			problems = append(problems, workspace+" sets "+key+", which can move downloads off "+npmRegistry)
		}
	}
	sort.Strings(problems)
	return problems
}

func versionParts3(s string) ([3]int, bool) {
	var v [3]int
	if !pinnedGo(s) {
		return v, false
	}
	for i, part := range strings.Split(s, ".") {
		v[i], _ = strconv.Atoi(part)
	}
	return v, true
}

func atLeast(v, min [3]int) bool {
	for i := range v {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

func miseTools(inputs map[string]Snapshot) map[string]any {
	var mise struct {
		Tools map[string]any `toml:"tools"`
	}
	_, _ = toml.Decode(inputs[".standards/mise.project.toml"].Content, &mise)
	return mise.Tools
}

func packageManager(inputs map[string]Snapshot, dir, name string) (string, bool) {
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	_ = json.Unmarshal([]byte(inputs[path.Join(dir, "package.json")].Content), &pkg)
	version, ok := strings.CutPrefix(pkg.PackageManager, name+"@")
	version, _, _ = strings.Cut(version, "+")
	return version, ok
}

// npmVersion reports whether the npm that runs here is known to support
// min-release-age, added in npm 11.10.0. The evidence is what mise installs:
// an npm pin, or else the npm bundled with the pinned Node.js, where 24.14.1,
// 25.7.0 and every 26 release are the first to bundle npm 11.10 or later.
// packageManager is not evidence: npm does not switch to the version it
// names, and the generated bootstrap and CI call npm directly.
func npmVersion(inputs map[string]Snapshot) string {
	supported := "npm 11.10 or later"
	tools := miseTools(inputs)
	if pin, pinned := tools["npm"]; pinned {
		version, _ := pin.(string)
		if v, exact := versionParts3(version); exact && atLeast(v, [3]int{11, 10, 0}) {
			return supported
		} else if exact {
			return "npm older than 11.10"
		}
		return ""
	}
	node, _ := tools["node"].(string)
	v, exact := versionParts3(node)
	if !exact {
		return ""
	}
	if v[0] >= 26 || (v[0] == 25 && atLeast(v, [3]int{25, 7, 0})) || (v[0] == 24 && atLeast(v, [3]int{24, 14, 1})) {
		return supported
	}
	return "npm older than 11.10"
}

func npmCooldown(npmrc string, settings map[string]string, version string) finding {
	days, err := strconv.ParseFloat(settings["min-release-age"], 64)
	if err != nil || !(days > 0) {
		return finding{"warn", npmrc, "min-release-age is not set to a positive number of days"}
	}
	set := "min-release-age=" + strconv.FormatFloat(days, 'f', -1, 64) + " days"
	switch version {
	case "npm 11.10 or later":
		return finding{"pass", npmrc, set + " with " + version}
	case "":
		return finding{"warn", npmrc, set + ", but mise pins neither npm nor an exact Node.js version, and npm older than 11.10 ignores the setting"}
	}
	return finding{"warn", npmrc, set + ", but the pinned tools run " + version + ", which ignores the setting"}
}

func npmScripts(npmrc string, settings map[string]string) finding {
	if settings["ignore-scripts"] != "true" {
		return finding{"warn", npmrc, "ignore-scripts=true is not set"}
	}
	return finding{"pass", npmrc, "ignore-scripts=true"}
}

// pnpmMajor prefers packageManager, which pnpm itself enforces, over mise.
func pnpmMajor(inputs map[string]Snapshot, dir string) int {
	version, ok := packageManager(inputs, dir, "pnpm")
	if !ok {
		version, _ = miseTools(inputs)["pnpm"].(string)
	}
	v, _ := versionParts3(version)
	return v[0]
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func pnpmSettings(workspace string, cfg map[string]any, parsed bool, major int) (cooldown, scripts finding) {
	if !parsed {
		f := finding{"warn", workspace, "cannot be read or parsed"}
		return f, f
	}
	safeDefaults := major >= pnpmSafeDefaultsMajor
	version := "the pnpm version is not pinned in packageManager or mise"
	if major > 0 {
		version = fmt.Sprintf("pnpm %d", major)
	}
	age, explicit := cfg["minimumReleaseAge"]
	minutes, numeric := number(age)
	strict, strictSet := cfg["minimumReleaseAgeStrict"].(bool)
	switch {
	case explicit && !(numeric && minutes > 0):
		cooldown = finding{"warn", workspace, "minimumReleaseAge is not a positive number of minutes"}
	case !explicit && !safeDefaults:
		cooldown = finding{"warn", workspace, "minimumReleaseAge is not set and " + version + " has no default cooldown"}
	case strictSet && !strict:
		cooldown = finding{"warn", workspace, "minimumReleaseAgeStrict is false, so pnpm falls back to a version inside the cooldown"}
	case !explicit && !strict:
		// 既定の 1440 分は strict でなく、範囲内に古い版がなければ新しい版を取り込む。
		cooldown = finding{"warn", workspace, version + " applies its default minimumReleaseAge without minimumReleaseAgeStrict, so it falls back to a version inside the cooldown"}
	case !explicit:
		cooldown = finding{"pass", workspace, version + " applies its default minimumReleaseAge of 1440 minutes with minimumReleaseAgeStrict"}
	default:
		cooldown = finding{"pass", workspace, "minimumReleaseAge=" + strconv.FormatFloat(minutes, 'f', -1, 64) + " minutes, enforced strictly"}
	}
	switch {
	case cfg["dangerouslyAllowAllBuilds"] != nil && cfg["dangerouslyAllowAllBuilds"] != false:
		scripts = finding{"warn", workspace, "dangerouslyAllowAllBuilds runs every dependency build script"}
	case safeDefaults:
		allowed := 0
		if builds, ok := cfg["allowBuilds"].(map[string]any); ok {
			for _, v := range builds {
				if v == true {
					allowed++
				}
			}
		}
		scripts = finding{"pass", workspace, fmt.Sprintf("%s blocks dependency build scripts unless allowBuilds permits them (%d permitted)", version, allowed)}
	default:
		scripts = finding{"warn", workspace, "allowBuilds needs pnpm 11 or later, and " + version}
	}
	return cooldown, scripts
}

var (
	isoDuration      = regexp.MustCompile(`^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)
	friendlyDuration = regexp.MustCompile(`^(\d+)\s*(weeks?|wks?|w|days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\s*,?\s*`)
)

// cooldownSeconds parses the durations uv documents for exclude-newer: ISO
// 8601 such as P7D, or friendly ones such as "7 days". Anything else is not
// taken as a cooldown, because uv does not reject every value it cannot use.
func cooldownSeconds(s string) (int64, bool) {
	units := []int64{7 * 86400, 86400, 3600, 60, 1}
	var total int64
	if m := isoDuration.FindStringSubmatch(s); m != nil && s != "P" && !strings.HasSuffix(s, "T") {
		for i, digits := range m[1:] {
			n, err := strconv.ParseInt("0"+digits, 10, 32)
			if err != nil {
				return 0, false
			}
			total += n * units[i]
		}
		return total, true
	}
	rest := strings.TrimSpace(s)
	if rest == "" {
		return 0, false
	}
	for rest != "" {
		m := friendlyDuration.FindStringSubmatch(rest)
		if m == nil {
			return 0, false
		}
		n, err := strconv.ParseInt(m[1], 10, 32)
		if err != nil {
			return 0, false
		}
		total += n * units[strings.Index("wdhms", m[2][:1])]
		rest = rest[len(m[0]):]
	}
	return total, true
}

// uv reads uv.toml instead of [tool.uv] when both are in the directory.
func uvCooldown(ctx dependencyContext, dir string) finding {
	config := path.Join(dir, "uv.toml")
	var value any
	if ctx.unreadable[config] {
		return finding{"warn", config, "cannot be read safely, and it takes precedence over pyproject.toml"}
	} else if ctx.inputs[config].Exists {
		var cfg map[string]any
		if _, err := toml.Decode(ctx.inputs[config].Content, &cfg); err != nil {
			return finding{"warn", config, "cannot be parsed, and it takes precedence over pyproject.toml"}
		}
		value = cfg["exclude-newer"]
	} else {
		config = path.Join(dir, "pyproject.toml")
		var cfg struct {
			Tool struct {
				UV map[string]any `toml:"uv"`
			} `toml:"tool"`
		}
		if _, err := toml.Decode(ctx.inputs[config].Content, &cfg); err != nil {
			return finding{"warn", config, "cannot be parsed"}
		}
		value = cfg.Tool.UV["exclude-newer"]
	}
	s, ok := value.(string)
	if !ok {
		return finding{"warn", config, "exclude-newer is not set"}
	}
	if seconds, ok := cooldownSeconds(s); ok {
		if seconds <= 0 {
			return finding{"warn", config, "exclude-newer is a zero duration"}
		}
		if seconds%86400 == 0 {
			return finding{"pass", config, fmt.Sprintf("exclude-newer is a cooldown of %d days", seconds/86400)}
		}
		return finding{"pass", config, fmt.Sprintf("exclude-newer is a cooldown of %d seconds", seconds)}
	}
	if cutoff, err := time.Parse(time.RFC3339, s); err == nil {
		if cutoff.Before(ctx.now) {
			return finding{"pass", config, "exclude-newer is a fixed cutoff in the past"}
		}
		return finding{"warn", config, "exclude-newer is a cutoff in the future"}
	}
	return finding{"warn", config, "exclude-newer is not a duration or an RFC 3339 timestamp this check recognizes"}
}

// pnpm 12 can write a boot document before the project's dependency graph, so
// every document is checked instead of the first one.
func pnpmLockViolations(content string, local func(string) bool) ([]string, error) {
	var found []string
	dec := yaml.NewDecoder(strings.NewReader(content))
	for {
		var doc struct {
			Packages map[string]struct {
				Resolution map[string]any `yaml:"resolution"`
			} `yaml:"packages"`
		}
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		for name, pkg := range doc.Packages {
			r := pkg.Resolution
			if r["type"] == "directory" {
				if dir, _ := r["directory"].(string); !local(dir) {
					found = append(found, name)
				}
				continue
			}
			tarball, hasTarball := r["tarball"].(string)
			integrity, _ := r["integrity"].(string)
			_, git := r["repo"]
			if git || r["type"] != nil || (hasTarball && !strings.HasPrefix(tarball, npmRegistry)) || !strings.HasPrefix(integrity, "sha512-") {
				found = append(found, name)
			}
		}
	}
	return found, nil
}
