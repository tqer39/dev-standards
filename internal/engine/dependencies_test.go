package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	bundle "github.com/tqer39/dev-standards"
)

const sha512 = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="

const npmLockOK = `{"lockfileVersion":3,"packages":{
"":{"name":"sample"},
"node_modules/a":{"version":"1.0.0","resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz","integrity":"` + sha512 + `"},
"node_modules/a/node_modules/bundled":{"version":"1.0.0","inBundle":true},
"node_modules/local":{"resolved":"packages/local","link":true},
"packages/local":{"version":"1.0.0"}
}}`

const pnpmLockOK = "lockfileVersion: '9.0'\npackages:\n  a@1.0.0:\n    resolution: {integrity: " + sha512 + "}\n  local@file:local:\n    resolution: {directory: local, type: directory}\n"

const uvLockOK = `version = 1

[[package]]
name = "sample"
version = "0.1.0"
source = { editable = "." }

[[package]]
name = "a"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }
sdist = { url = "https://files.pythonhosted.org/packages/a-1.0.0.tar.gz", hash = "sha256:00" }
wheels = [
    { url = "https://files.pythonhosted.org/packages/a-1.0.0-py3-none-any.whl", hash = "sha256:11" },
]
`

const nodeTools = "[tools]\nnode = \"24.21.0\"\n"

// 合成の値。実在の認証情報ではない。
const marker = "fixtureCredentialMarker"

func TestLockfileSourceViolations(t *testing.T) {
	local := func(rel string) bool { return rel == "." || rel == "packages/local" || rel == "local" }
	npm := func(entry string) string {
		return `{"lockfileVersion":3,"packages":{"node_modules/a":` + entry + `}}`
	}
	uv := func(source, files string) string {
		return "version = 1\n[[package]]\nname = \"a\"\nsource = " + source + "\n" + files
	}
	type check = func(string, func(string) bool) ([]string, error)
	for name, c := range map[string]struct {
		check   check
		content string
		want    []string
		invalid bool
	}{
		"npm ok":             {npmLockViolations, npmLockOK, nil, false},
		"npm git":            {npmLockViolations, npm(`{"resolved":"git+ssh://git@github.com/o/a.git#0123","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm other host":     {npmLockViolations, npm(`{"resolved":"https://registry.npmjs.org.example.com/a.tgz","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm http":           {npmLockViolations, npm(`{"resolved":"http://registry.npmjs.org/a/-/a-1.0.0.tgz","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm local tarball":  {npmLockViolations, npm(`{"resolved":"file:vendor/a.tgz","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm sha1":           {npmLockViolations, npm(`{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz","integrity":"sha1-AAAA"}`), []string{"node_modules/a"}, false},
		"npm no integrity":   {npmLockViolations, npm(`{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz"}`), []string{"node_modules/a"}, false},
		"npm no resolved":    {npmLockViolations, npm(`{"version":"1.0.0","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm remote link":    {npmLockViolations, npm(`{"resolved":"git+https://example.com/a.git","link":true}`), []string{"node_modules/a"}, false},
		"npm outside link":   {npmLockViolations, npm(`{"resolved":"../../outside","link":true}`), []string{"node_modules/a"}, false},
		"npm outside target": {npmLockViolations, `{"lockfileVersion":3,"packages":{"../outside":{"version":"1.0.0"}}}`, []string{"../outside"}, false},
		"npm version 1":      {npmLockViolations, `{"lockfileVersion":1,"dependencies":{}}`, nil, true},
		"npm broken":         {npmLockViolations, `{`, nil, true},

		"pnpm ok":                {pnpmLockViolations, pnpmLockOK, nil, false},
		"pnpm tarball":           {pnpmLockViolations, "packages:\n  a@https://example.com/a.tgz:\n    resolution: {tarball: https://example.com/a.tgz}\n", []string{"a@https://example.com/a.tgz"}, false},
		"pnpm git":               {pnpmLockViolations, "packages:\n  a@git:\n    resolution: {commit: abc, repo: https://example.com/a.git, type: git}\n", []string{"a@git"}, false},
		"pnpm sha1":              {pnpmLockViolations, "packages:\n  a@1.0.0:\n    resolution: {integrity: sha1-AAAA}\n", []string{"a@1.0.0"}, false},
		"pnpm outside directory": {pnpmLockViolations, "packages:\n  a@file:../outside:\n    resolution: {directory: ../outside, type: directory}\n", []string{"a@file:../outside"}, false},
		"pnpm second document":   {pnpmLockViolations, "---\nlockfileVersion: '9.0'\npackages: {}\n---\n" + "packages:\n  a@1.0.0:\n    resolution: {tarball: https://example.com/a.tgz, integrity: " + sha512 + "}\n", []string{"a@1.0.0"}, false},
		"pnpm broken":            {pnpmLockViolations, "packages: [", nil, true},

		"uv ok":                {uvLockViolations, uvLockOK, nil, false},
		"uv git":               {uvLockViolations, uv(`{ git = "https://example.com/a.git?rev=abc#abc" }`, ""), []string{"a"}, false},
		"uv url":               {uvLockViolations, uv(`{ url = "https://example.com/a-1.0.0.tar.gz" }`, ""), []string{"a"}, false},
		"uv other index":       {uvLockViolations, uv(`{ registry = "https://example.com/simple" }`, ""), []string{"a"}, false},
		"uv file host":         {uvLockViolations, uv(`{ registry = "https://pypi.org/simple" }`, `wheels = [{ url = "https://example.com/a.whl", hash = "sha256:11" }]`), []string{"a"}, false},
		"uv no hash":           {uvLockViolations, uv(`{ registry = "https://pypi.org/simple" }`, `sdist = { url = "https://files.pythonhosted.org/a.tar.gz" }`), []string{"a"}, false},
		"uv outside directory": {uvLockViolations, uv(`{ directory = "../outside" }`, ""), []string{"a"}, false},
		"uv outside editable":  {uvLockViolations, uv(`{ editable = "/abs/outside" }`, ""), []string{"a"}, false},
		"uv broken":            {uvLockViolations, "version = [", nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := c.check(c.content, local)
			sort.Strings(got)
			if (err != nil) != c.invalid || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestRepositoryPathRejectsReferencesThatLeaveTheRepository(t *testing.T) {
	for rel, want := range map[string]string{".": "web", "lib": "web/lib", "../shared": "shared", "./a/../b": "web/b"} {
		if got, ok := repositoryPath("web", rel); !ok || got != want {
			t.Errorf("%q: %q %v", rel, got, ok)
		}
	}
	for _, rel := range []string{"", "../..", "../../outside", "/abs", "C:\\abs", "file:../x", "..\\outside"} {
		if got, ok := repositoryPath("web", rel); ok {
			t.Errorf("accepted %q as %q", rel, got)
		}
	}
}

func TestCooldownSeconds(t *testing.T) {
	for s, want := range map[string]int64{"7 days": 604800, "1 week": 604800, "24 hours": 86400, "1w 2d": 777600, "2h30m": 9000, "P7D": 604800, "PT24H": 86400, "P1W": 604800, "P1DT12H": 129600, "0 days": 0, "PT0S": 0} {
		if got, ok := cooldownSeconds(s); !ok || got != want {
			t.Errorf("%q: %d %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "invalid", "P", "PT", "P1DT", "1 month", "7", "days", "-7 days", "2026-01-01T00:00:00Z", "99999999999999999999 days"} {
		if got, ok := cooldownSeconds(s); ok {
			t.Errorf("accepted %q as %d", s, got)
		}
	}
}

func TestDependencyIntakeSettings(t *testing.T) {
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	status := func(inputs map[string]string) (cooldown, scripts, source string) {
		ctx := dependencyContext{inputs: map[string]Snapshot{}, unreadable: map[string]bool{}, now: now}
		ctx.localDir = func(p string) bool {
			return p == "." || p == "packages/local" || p == "local" || p == "api" || p == "web/local"
		}
		for name, content := range inputs {
			if content == "unreadable" {
				ctx.unreadable[name] = true
				continue
			}
			ctx.inputs[name] = file(content)
		}
		r := dependencyResults(ctx, "main")
		return r[0].Status, r[1].Status, r[2].Status
	}
	const mise = ".standards/mise.project.toml"
	const pnpm11 = `{"packageManager":"pnpm@11.20.0+sha512.abc"}`
	npmSet := "min-release-age=7\nignore-scripts=true\n"
	for name, c := range map[string]struct {
		inputs                    map[string]string
		cooldown, scripts, source string
	}{
		"nothing to judge": {map[string]string{"go.mod": "module example.com/a\n"}, "not_applicable", "not_applicable", "not_applicable"},
		"npm unset":        {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools}, "warn", "warn", "pass"},
		"npm other keys":   {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": "# min-release-age=7\n;ignore-scripts=true\nregistry=https://registry.npmjs.org/\n"}, "warn", "warn", "pass"},
		"npm disabled":     {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": "min-release-age=0\nignore-scripts=false\n"}, "warn", "warn", "pass"},
		"npm set":          {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": "min-release-age = 7\r\nignore-scripts=true\r\n"}, "pass", "pass", "pass"},
		"npm last wins":    {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": "ignore-scripts=true\nignore-scripts=false\nmin-release-age=7\n"}, "pass", "warn", "pass"},
		"npm unreadable":   {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": "unreadable"}, "warn", "warn", "fail"},
		// 別のディレクトリの .npmrc は、その lockfile の導入には効かない。
		"npm other directory": {map[string]string{"web/package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet}, "warn", "warn", "fail"},

		// min-release-age は npm 11.10.0 で入った。それより前の npm は設定を無視する。
		"npm 10 from node 22":    {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"22.23.4\"\n", ".npmrc": npmSet}, "warn", "pass", "pass"},
		"npm before 24.14.1":     {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"24.14.0\"\n", ".npmrc": npmSet}, "warn", "pass", "pass"},
		"npm from node 24.14.1":  {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"24.14.1\"\n", ".npmrc": npmSet}, "pass", "pass", "pass"},
		"npm from node 26":       {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"26.0.0\"\n", ".npmrc": npmSet}, "pass", "pass", "pass"},
		"npm unknown version":    {map[string]string{"package-lock.json": npmLockOK, ".npmrc": npmSet}, "warn", "pass", "pass"},
		"npm loose node":         {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"24\"\n", ".npmrc": npmSet}, "warn", "pass", "pass"},
		"npm packageManager old": {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, "package.json": `{"packageManager":"npm@11.9.0"}`, ".npmrc": npmSet}, "warn", "pass", "pass"},
		"npm packageManager new": {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"22.23.4\"\n", "package.json": `{"packageManager":"npm@11.10.0+sha512.abc"}`, ".npmrc": npmSet}, "pass", "pass", "pass"},
		"npm mise npm pin":       {map[string]string{"package-lock.json": npmLockOK, mise: "[tools]\nnode = \"22.23.4\"\nnpm = \"11.19.0\"\n", ".npmrc": npmSet}, "pass", "pass", "pass"},

		// lock が公式の URL でも、registry の設定で実際の取得先を変えられる。
		"npm registry replaced":     {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet + "registry=https://registry.example.com/\n"}, "pass", "pass", "fail"},
		"npm registry from env":     {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet + "registry=${NPM_REGISTRY}\n"}, "pass", "pass", "fail"},
		"npm registry overridden":   {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet + "registry=https://registry.npmjs.org/\nregistry=http://registry.npmjs.org/\n"}, "pass", "pass", "fail"},
		"npm scoped registry":       {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet + "@corp:registry=https://npm.example.com/\n"}, "pass", "pass", "fail"},
		"npm official registries":   {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet + "registry=https://registry.npmjs.org\n@corp:registry = \"https://registry.npmjs.org/\"\n"}, "pass", "pass", "pass"},
		"pnpm npmrc registry":       {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, ".npmrc": "registry=https://registry.example.com/\n"}, "warn", "pass", "fail"},
		"pnpm npmrc unreadable":     {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, ".npmrc": "unreadable"}, "warn", "pass", "fail"},
		"pnpm workspace registry":   {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "registry: https://registry.example.com/\n"}, "warn", "pass", "fail"},
		"pnpm registries by url":    {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "registries:\n  https://npm.example.com/:\n    scopes: [\"@corp\"]\n"}, "warn", "pass", "fail"},
		"pnpm registries by scope":  {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "registries:\n  default: https://registry.npmjs.org/\n  \"@corp\": https://npm.example.com/\n"}, "warn", "pass", "fail"},
		"pnpm official registries":  {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "registry: https://registry.npmjs.org/\nregistries:\n  default: https://registry.npmjs.org/\n"}, "warn", "pass", "pass"},
		"pnpm official declaration": {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "registries:\n  https://registry.npmjs.org/:\n    networkConcurrency: 8\n"}, "warn", "pass", "pass"},
		"pnpm named registries":     {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "namedRegistries:\n  work: https://npm.example.com/\n"}, "warn", "pass", "fail"},
		"pnpm pnpr server":          {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "pnprServer: https://pnpr.example.com/\n"}, "warn", "pass", "fail"},

		// pnpm の既定の 1440 分は strict でなく、公開直後の版に fallback できる。
		"pnpm 11 defaults":         {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11}, "warn", "pass", "pass"},
		"pnpm 12 from mise":        {map[string]string{"web/pnpm-lock.yaml": pnpmLockOK, mise: "[tools]\npnpm = \"12.18.0\"\n"}, "warn", "pass", "pass"},
		"pnpm default made strict": {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "minimumReleaseAgeStrict: true\n"}, "pass", "pass", "pass"},
		"pnpm explicit":            {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "minimumReleaseAge: 10080\n"}, "pass", "pass", "pass"},
		"pnpm explicit not strict": {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "minimumReleaseAge: 10080\nminimumReleaseAgeStrict: false\n"}, "warn", "pass", "pass"},
		"pnpm age as text":         {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "minimumReleaseAge: \"" + marker + "\"\n"}, "warn", "pass", "pass"},
		"pnpm 10":                  {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@10.33.0"}`}, "warn", "warn", "pass"},
		"pnpm 10 strict only":      {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@10.33.0"}`, "pnpm-workspace.yaml": "minimumReleaseAgeStrict: true\n"}, "warn", "warn", "pass"},
		"pnpm 10 explicit":         {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@10.33.0"}`, "pnpm-workspace.yaml": "minimumReleaseAge: 10080\n"}, "pass", "warn", "pass"},
		"pnpm unpinned":            {map[string]string{"pnpm-lock.yaml": pnpmLockOK}, "warn", "warn", "pass"},
		"pnpm opted out":           {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "minimumReleaseAge: 0\ndangerouslyAllowAllBuilds: true\n"}, "warn", "warn", "pass"},
		"pnpm allow list":          {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "minimumReleaseAge: 1440\nallowBuilds:\n  esbuild: true\n  core-js: false\n"}, "pass", "pass", "pass"},
		"pnpm broken config":       {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": pnpm11, "pnpm-workspace.yaml": "packages: ["}, "warn", "warn", "fail"},
		"pnpm tarball source":      {map[string]string{"pnpm-lock.yaml": "packages:\n  a@1.0.0:\n    resolution: {tarball: https://example.com/a.tgz}\n", "package.json": pnpm11}, "warn", "pass", "fail"},
		"pnpm directory inside":    {map[string]string{"web/pnpm-lock.yaml": pnpmLockOK, "web/package.json": pnpm11}, "warn", "pass", "pass"},
		"pnpm directory missing":   {map[string]string{"other/pnpm-lock.yaml": pnpmLockOK, "other/package.json": pnpm11}, "warn", "pass", "fail"},

		"uv unset":    {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[project]\nname = \"sample\"\n"}, "warn", "not_applicable", "pass"},
		"uv disabled": {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = false\n"}, "warn", "not_applicable", "pass"},
		"uv relative": {map[string]string{"api/uv.lock": uvLockOK, "api/pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n"}, "pass", "not_applicable", "pass"},
		// 値が待機として働くときだけ合格にする。
		"uv zero":    {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"0 days\"\n"}, "warn", "not_applicable", "pass"},
		"uv invalid": {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"invalid\"\n"}, "warn", "not_applicable", "pass"},
		"uv future":  {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"2099-01-01T00:00:00Z\"\n"}, "warn", "not_applicable", "pass"},
		"uv past":    {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"2026-01-01T00:00:00Z\"\n"}, "pass", "not_applicable", "pass"},
		"uv iso":     {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"P7D\"\n"}, "pass", "not_applicable", "pass"},
		// uv.toml があると、uv は pyproject.toml の [tool.uv] を読まない。
		"uv.toml disables":   {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n", "uv.toml": "exclude-newer = false\n"}, "warn", "not_applicable", "pass"},
		"uv.toml silent":     {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n", "uv.toml": "native-tls = true\n"}, "warn", "not_applicable", "pass"},
		"uv.toml sets":       {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[project]\nname = \"sample\"\n", "uv.toml": "exclude-newer = \"7 days\"\n"}, "pass", "not_applicable", "pass"},
		"uv.toml unreadable": {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n", "uv.toml": "unreadable"}, "warn", "not_applicable", "pass"},
		"uv.toml broken":     {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n", "uv.toml": "exclude-newer = ["}, "warn", "not_applicable", "pass"},
		"uv.toml elsewhere":  {map[string]string{"api/uv.lock": uvLockOK, "api/pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n", "uv.toml": "exclude-newer = false\n"}, "pass", "not_applicable", "pass"},

		// 1 つでも不足があれば、全体をその状態で報告する。
		"mixed": {map[string]string{"package-lock.json": npmLockOK, mise: nodeTools, ".npmrc": npmSet, "api/uv.lock": uvLockOK, "api/pyproject.toml": "[project]\nname = \"api\"\n"}, "warn", "pass", "pass"},
	} {
		t.Run(name, func(t *testing.T) {
			cooldown, scripts, source := status(c.inputs)
			if cooldown != c.cooldown || scripts != c.scripts || source != c.source {
				t.Fatalf("cooldown=%s install-scripts=%s source=%s", cooldown, scripts, source)
			}
		})
	}
}

// lock のキー、設定の値、パーサーのエラーは、報告とエラーに転載しない。
func TestDependencyEvidenceNeverQuotesInputs(t *testing.T) {
	url := "https://user:" + marker + "@example.com/a.tgz?token=" + marker + "#" + marker
	ctx := dependencyContext{unreadable: map[string]bool{}, localDir: func(string) bool { return false }, now: time.Now(), inputs: map[string]Snapshot{
		"package.json":               file(`{"packageManager":"pnpm@11.20.0"}`),
		"pnpm-lock.yaml":             file("packages:\n  'a@" + url + "':\n    resolution: {tarball: '" + url + "'}\n  '" + marker + "':\n    resolution: {directory: '" + marker + "', type: directory}\n  b@1.0.0:\n    resolution: {integrity: sha1-AAAA}\n"),
		"pnpm-workspace.yaml":        file("minimumReleaseAge: " + marker + "\nminimumReleaseAgeStrict: " + marker + "\nregistry: " + url + "\nregistries:\n  '" + url + "': {}\nnamedRegistries:\n  " + marker + ": " + url + "\npnprServer: " + url + "\n"),
		".npmrc":                     file("registry=" + url + "\n@" + marker + ":registry=" + url + "\nmin-release-age=" + marker + "\n//example.com/:_authToken=" + marker + "\n"),
		"web/package-lock.json":      file(`{"lockfileVersion":3,"packages":{"node_modules/` + marker + ` x":{"resolved":"` + url + `"},"node_modules/a":{"resolved":"` + url + `","link":true}}}`),
		"web/.npmrc":                 file("min-release-age=7\nregistry = " + url + "\n"),
		"api/uv.lock":                file("version = 1\n[[package]]\nname = \"" + url + "\"\nsource = { url = \"" + url + "\" }\n[[package]]\nname = \"b\"\nsource = { directory = \"/" + marker + "\" }\n"),
		"api/pyproject.toml":         file("[tool.uv]\nexclude-newer = \"" + marker + "\"\n"),
		"broken/uv.lock":             file("version = \"" + marker + "\nname = [" + marker),
		"broken/uv.toml":             file("exclude-newer = \"" + marker + "\nx = [" + marker),
		"broken/pnpm-lock.yaml":      file("packages:\n  " + marker + ": [" + marker),
		"broken/pnpm-workspace.yaml": file(marker + ": [" + marker),
		"broken/package-lock.json":   file(`{"lockfileVersion":"` + marker + `"`),
	}}
	results := dependencyResults(ctx, "main")
	if text := string(JSON(results)); strings.Contains(text, marker) || strings.Contains(text, "example.com") {
		t.Fatalf("evidence quotes its inputs: %s", text)
	}
	if results[2].Status != "fail" || !strings.Contains(results[2].Evidence, "b@1.0.0") || !strings.Contains(results[2].Evidence, "name withheld") {
		t.Fatalf("violations lost their safe identifiers: %+v", results[2])
	}
}

func dependencyStatuses(t *testing.T, r Report) map[string]Result {
	t.Helper()
	found := map[string]Result{}
	for _, result := range r.Results {
		if result.Rule == dependencyIntakeRule || result.Rule == lockfileSourceRule {
			found[result.Check] = result
		}
	}
	if len(found) != 3 {
		t.Fatalf("dependency results: %+v", found)
	}
	return found
}

// 初回適用、再適用、設定の追加、lock の改ざんで、判定と終了コードがどう変わるかを通して確かめる。
func TestCheckJudgesDependencyIntakeAndLockfileSources(t *testing.T) {
	tree, req := projectFixture(t)
	write(t, tree.Root, "mise.toml", strings.Replace(read(t, tree.Root, "mise.toml"), "[tools]\n", "[tools]\nnode = \"24.21.0\"\n", 1))
	write(t, tree.Root, "package.json", `{"name":"root"}`)
	write(t, tree.Root, "packages/local/index.js", "")
	write(t, tree.Root, "web/local/index.js", "")
	write(t, tree.Root, "package-lock.json", npmLockOK)
	write(t, tree.Root, "web/pnpm-lock.yaml", pnpmLockOK)
	write(t, tree.Root, "api/uv.lock", uvLockOK)
	const secret = "//registry.npmjs.org/:_authToken=" + marker
	write(t, tree.Root, ".npmrc", secret+"\n")
	write(t, tree.Root, "api/uv.toml", "[[index]]\nurl = \"https://user:"+marker+"@example.com/simple\"\n")
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".npmrc", "api/uv.toml"} {
		if _, listed := plan.Inputs[name]; listed {
			t.Fatalf("the plan lists %s", name)
		}
	}
	if strings.Contains(string(JSON(plan)), marker) {
		t.Fatal("the plan exposes settings that can hold credentials")
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	commit(t, tree.Root)
	check := func() (map[string]Result, error) {
		t.Helper()
		report, err := tree.Check()
		text := string(JSON(report))
		if err != nil {
			text += err.Error()
		}
		if strings.Contains(text, marker) {
			t.Fatalf("the report or the error exposes its inputs: %s", text)
		}
		return dependencyStatuses(t, report), err
	}

	// 設定が足りないだけでは、採用版を上げた利用先の必須チェックを落とさない。
	found, err := check()
	if err != nil {
		t.Fatalf("missing settings failed the check: %v", err)
	}
	if found["cooldown"].Status != "warn" || found["install-scripts"].Status != "warn" || found["source"].Status != "pass" {
		t.Fatalf("unexpected statuses: %+v", found)
	}
	for _, want := range []string{".npmrc: min-release-age", "web/pnpm-workspace.yaml: minimumReleaseAge", "api/uv.toml: exclude-newer is not set"} {
		if !strings.Contains(found["cooldown"].Evidence, want) {
			t.Errorf("cooldown evidence lacks %q: %s", want, found["cooldown"].Evidence)
		}
	}

	// 利用先が自分の設定を足しても、生成物との差分や衝突にならない。
	write(t, tree.Root, ".npmrc", secret+"\nmin-release-age=7\nignore-scripts=true\n")
	write(t, tree.Root, "web/package.json", `{"name":"web","packageManager":"pnpm@11.20.0"}`)
	write(t, tree.Root, "web/pnpm-workspace.yaml", "minimumReleaseAge: 10080\n")
	write(t, tree.Root, "api/uv.toml", "exclude-newer = \"7 days\"\n")
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("settings changed the plan: %+v %v", again, err)
	}
	if found, err = check(); err != nil {
		t.Fatal(err)
	}
	for name, result := range found {
		if result.Status != "pass" {
			t.Errorf("%s: %+v", name, result)
		}
	}
	rejected := func(what, evidence string) {
		t.Helper()
		found, err := check()
		if ExitCode(err) != 1 || !strings.Contains(err.Error(), lockfileSourceRule) {
			t.Fatalf("%s was accepted: %v", what, err)
		}
		if found["source"].Status != "fail" || !strings.Contains(found["source"].Evidence, evidence) || found["cooldown"].Status != "pass" {
			t.Fatalf("%s: unexpected statuses: %+v", what, found)
		}
	}

	// 取得元の差し替えは不適合として終了コード 1 にし、ほかの判定は報告に残す。
	write(t, tree.Root, "package-lock.json", strings.Replace(npmLockOK, "https://registry.npmjs.org/a/-/a-1.0.0.tgz", "https://user:"+marker+"@example.com/a-1.0.0.tgz", 1))
	rejected("a replaced source", "package-lock.json: 1 packages are not fetched as required: node_modules/a")
	if _, err := tree.Verify(nil); ExitCode(err) != 1 {
		t.Fatalf("verify ran project tasks after a failed check: %v", err)
	}
	write(t, tree.Root, "package-lock.json", npmLockOK)

	// lock を変えずに registry の設定だけを変える回避も、不適合にする。
	write(t, tree.Root, ".npmrc", secret+"\nmin-release-age=7\nignore-scripts=true\nregistry=https://user:"+marker+"@example.com/\n")
	rejected("a replaced npm registry", ".npmrc sets registry to something other than https://registry.npmjs.org/")
	write(t, tree.Root, ".npmrc", secret+"\nmin-release-age=7\nignore-scripts=true\n")
	write(t, tree.Root, "web/.npmrc", "@corp:registry=https://user:"+marker+"@example.com/\n")
	rejected("a replaced pnpm registry", "web/.npmrc sets a scoped registry")
	if err := os.Remove(filepath.Join(tree.Root, "web/.npmrc")); err != nil {
		t.Fatal(err)
	}

	// リポジトリ内の通常のディレクトリだけを、取得のない依存として扱う。
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(tree.Root, "packages/local")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree.Root, "packages/local")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	rejected("a link through a symlink that leaves the repository", "node_modules/local")
	if err := os.Remove(filepath.Join(tree.Root, "packages/local")); err != nil {
		t.Fatal(err)
	}
	rejected("a link to a missing directory", "node_modules/local")
	write(t, tree.Root, "packages/local/index.js", "")
	if _, err := check(); err != nil {
		t.Fatal(err)
	}

	// 安全に読めない .npmrc は、実際の registry が分からないので合格にしない。
	if err := os.Remove(filepath.Join(tree.Root, ".npmrc")); err != nil {
		t.Fatal(err)
	}
	write(t, outside, "npmrc", "min-release-age=7\nignore-scripts=true\nregistry=https://user:"+marker+"@example.com/\n")
	if err := os.Symlink(filepath.Join(outside, "npmrc"), filepath.Join(tree.Root, ".npmrc")); err != nil {
		t.Fatal(err)
	}
	found, err = check()
	if ExitCode(err) != 1 || found["source"].Status != "fail" || found["cooldown"].Status != "warn" || !strings.Contains(found["cooldown"].Evidence, ".npmrc: cannot be read safely") {
		t.Fatalf("an unreadable .npmrc was accepted: %+v %v", found, err)
	}
}

func TestCheckReportsDependencyRulesAsNotApplicableWithoutLockfiles(t *testing.T) {
	tree := fixture(t)
	adopt(t, tree)
	report, err := tree.Check()
	if err != nil {
		t.Fatal(err)
	}
	for check, result := range dependencyStatuses(t, report) {
		if result.Status != "not_applicable" {
			t.Errorf("%s: %+v", check, result)
		}
	}
}

// check が報告する rule と check の組は、rules の宣言と一致させる。
func TestDependencyResultsMatchTheCatalog(t *testing.T) {
	rules, err := bundle.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, rule := range rules {
		if rule.ID != dependencyIntakeRule && rule.ID != lockfileSourceRule {
			continue
		}
		for _, check := range rule.Checks {
			declared[rule.ID+"/"+check.ID] = true
		}
	}
	results := dependencyResults(dependencyContext{}, "main")
	if len(results) != len(declared) {
		t.Fatalf("declared %v, reported %+v", declared, results)
	}
	for _, r := range results {
		if !declared[r.Rule+"/"+r.Check] {
			t.Errorf("undeclared check: %s/%s", r.Rule, r.Check)
		}
	}
}
