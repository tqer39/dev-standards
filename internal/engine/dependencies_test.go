package engine

import (
	"reflect"
	"strings"
	"testing"

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

func TestLockfileSourceViolations(t *testing.T) {
	npm := func(entry string) string {
		return `{"lockfileVersion":3,"packages":{"node_modules/a":` + entry + `}}`
	}
	uv := func(source, files string) string {
		return "version = 1\n[[package]]\nname = \"a\"\nsource = " + source + "\n" + files
	}
	for name, c := range map[string]struct {
		check   func(string) ([]string, error)
		content string
		want    []string
		invalid bool
	}{
		"npm ok":            {npmLockViolations, npmLockOK, nil, false},
		"npm git":           {npmLockViolations, npm(`{"resolved":"git+ssh://git@github.com/o/a.git#0123","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm other host":    {npmLockViolations, npm(`{"resolved":"https://registry.npmjs.org.example.com/a.tgz","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm http":          {npmLockViolations, npm(`{"resolved":"http://registry.npmjs.org/a/-/a-1.0.0.tgz","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm local tarball": {npmLockViolations, npm(`{"resolved":"file:vendor/a.tgz","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm sha1":          {npmLockViolations, npm(`{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz","integrity":"sha1-AAAA"}`), []string{"node_modules/a"}, false},
		"npm no integrity":  {npmLockViolations, npm(`{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz"}`), []string{"node_modules/a"}, false},
		"npm no resolved":   {npmLockViolations, npm(`{"version":"1.0.0","integrity":"` + sha512 + `"}`), []string{"node_modules/a"}, false},
		"npm remote link":   {npmLockViolations, npm(`{"resolved":"git+https://example.com/a.git","link":true}`), []string{"node_modules/a"}, false},
		"npm version 1":     {npmLockViolations, `{"lockfileVersion":1,"dependencies":{}}`, nil, true},
		"npm broken":        {npmLockViolations, `{`, nil, true},

		"pnpm ok":              {pnpmLockViolations, pnpmLockOK, nil, false},
		"pnpm tarball":         {pnpmLockViolations, "packages:\n  a@https://example.com/a.tgz:\n    resolution: {tarball: https://example.com/a.tgz}\n", []string{"a@https://example.com/a.tgz"}, false},
		"pnpm git":             {pnpmLockViolations, "packages:\n  a@git:\n    resolution: {commit: abc, repo: https://example.com/a.git, type: git}\n", []string{"a@git"}, false},
		"pnpm sha1":            {pnpmLockViolations, "packages:\n  a@1.0.0:\n    resolution: {integrity: sha1-AAAA}\n", []string{"a@1.0.0"}, false},
		"pnpm second document": {pnpmLockViolations, "---\nlockfileVersion: '9.0'\npackages: {}\n---\n" + "packages:\n  a@1.0.0:\n    resolution: {tarball: https://example.com/a.tgz, integrity: " + sha512 + "}\n", []string{"a@1.0.0"}, false},
		"pnpm broken":          {pnpmLockViolations, "packages: [", nil, true},

		"uv ok":          {uvLockViolations, uvLockOK, nil, false},
		"uv git":         {uvLockViolations, uv(`{ git = "https://example.com/a.git?rev=abc#abc" }`, ""), []string{"a"}, false},
		"uv url":         {uvLockViolations, uv(`{ url = "https://example.com/a-1.0.0.tar.gz" }`, ""), []string{"a"}, false},
		"uv other index": {uvLockViolations, uv(`{ registry = "https://example.com/simple" }`, ""), []string{"a"}, false},
		"uv file host":   {uvLockViolations, uv(`{ registry = "https://pypi.org/simple" }`, `wheels = [{ url = "https://example.com/a.whl", hash = "sha256:11" }]`), []string{"a"}, false},
		"uv no hash":     {uvLockViolations, uv(`{ registry = "https://pypi.org/simple" }`, `sdist = { url = "https://files.pythonhosted.org/a.tar.gz" }`), []string{"a"}, false},
		"uv broken":      {uvLockViolations, "version = [", nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := c.check(c.content)
			if (err != nil) != c.invalid || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestDependencyIntakeSettings(t *testing.T) {
	status := func(inputs map[string]string) (cooldown, scripts, source string) {
		snapshots := map[string]Snapshot{}
		for name, content := range inputs {
			snapshots[name] = file(content)
		}
		r := dependencyResults(snapshots, nil, "main")
		return r[0].Status, r[1].Status, r[2].Status
	}
	for name, c := range map[string]struct {
		inputs                    map[string]string
		cooldown, scripts, source string
	}{
		"nothing to judge": {map[string]string{"go.mod": "module example.com/a\n"}, "not_applicable", "not_applicable", "not_applicable"},
		"npm unset":        {map[string]string{"package-lock.json": npmLockOK}, "warn", "warn", "pass"},
		"npm other keys":   {map[string]string{"package-lock.json": npmLockOK, ".npmrc": "# min-release-age=7\n;ignore-scripts=true\nregistry=https://registry.npmjs.org/\n"}, "warn", "warn", "pass"},
		"npm disabled":     {map[string]string{"package-lock.json": npmLockOK, ".npmrc": "min-release-age=0\nignore-scripts=false\n"}, "warn", "warn", "pass"},
		"npm set":          {map[string]string{"package-lock.json": npmLockOK, ".npmrc": "min-release-age = 7\r\nignore-scripts=true\r\n"}, "pass", "pass", "pass"},
		"npm last wins":    {map[string]string{"package-lock.json": npmLockOK, ".npmrc": "ignore-scripts=true\nignore-scripts=false\nmin-release-age=7\n"}, "pass", "warn", "pass"},
		// 別のディレクトリの .npmrc は、その lockfile の導入には効かない。
		"npm other directory": {map[string]string{"web/package-lock.json": npmLockOK, ".npmrc": "min-release-age=7\nignore-scripts=true\n"}, "warn", "warn", "pass"},

		"pnpm 11 defaults":    {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@11.20.0+sha512.abc"}`}, "pass", "pass", "pass"},
		"pnpm 12 from mise":   {map[string]string{"web/pnpm-lock.yaml": pnpmLockOK, ".standards/mise.project.toml": "[tools]\npnpm = \"12.18.0\"\n"}, "pass", "pass", "pass"},
		"pnpm 10":             {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@10.33.0"}`}, "warn", "warn", "pass"},
		"pnpm 10 explicit":    {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@10.33.0"}`, "pnpm-workspace.yaml": "minimumReleaseAge: 10080\n"}, "pass", "warn", "pass"},
		"pnpm unpinned":       {map[string]string{"pnpm-lock.yaml": pnpmLockOK}, "warn", "warn", "pass"},
		"pnpm opted out":      {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@11.20.0"}`, "pnpm-workspace.yaml": "minimumReleaseAge: 0\ndangerouslyAllowAllBuilds: true\n"}, "warn", "warn", "pass"},
		"pnpm allow list":     {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@11.20.0"}`, "pnpm-workspace.yaml": "allowBuilds:\n  esbuild: true\n  core-js: false\n"}, "pass", "pass", "pass"},
		"pnpm broken config":  {map[string]string{"pnpm-lock.yaml": pnpmLockOK, "package.json": `{"packageManager":"pnpm@11.20.0"}`, "pnpm-workspace.yaml": "packages: ["}, "warn", "warn", "pass"},
		"pnpm tarball source": {map[string]string{"pnpm-lock.yaml": "packages:\n  a@1.0.0:\n    resolution: {tarball: https://example.com/a.tgz}\n", "package.json": `{"packageManager":"pnpm@11.20.0"}`}, "pass", "pass", "fail"},

		"uv unset":    {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[project]\nname = \"sample\"\n"}, "warn", "not_applicable", "pass"},
		"uv disabled": {map[string]string{"uv.lock": uvLockOK, "pyproject.toml": "[tool.uv]\nexclude-newer = false\n"}, "warn", "not_applicable", "pass"},
		"uv relative": {map[string]string{"api/uv.lock": uvLockOK, "api/pyproject.toml": "[tool.uv]\nexclude-newer = \"7 days\"\n"}, "pass", "not_applicable", "pass"},

		// 1 つでも不足があれば、全体をその状態で報告する。
		"mixed": {map[string]string{"package-lock.json": npmLockOK, ".npmrc": "min-release-age=7\nignore-scripts=true\n", "api/uv.lock": uvLockOK, "api/pyproject.toml": "[project]\nname = \"api\"\n"}, "warn", "pass", "pass"},
	} {
		t.Run(name, func(t *testing.T) {
			cooldown, scripts, source := status(c.inputs)
			if cooldown != c.cooldown || scripts != c.scripts || source != c.source {
				t.Fatalf("cooldown=%s install-scripts=%s source=%s", cooldown, scripts, source)
			}
		})
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
	write(t, tree.Root, "package.json", `{"name":"root"}`)
	write(t, tree.Root, "package-lock.json", npmLockOK)
	write(t, tree.Root, "web/pnpm-lock.yaml", pnpmLockOK)
	write(t, tree.Root, "api/uv.lock", uvLockOK)
	const secret = "//registry.npmjs.org/:_authToken=npm_notARealTokenOnlyAFixture"
	write(t, tree.Root, ".npmrc", secret+"\n")
	plan, err := tree.Plan(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, listed := plan.Inputs[".npmrc"]; listed || strings.Contains(string(JSON(plan)), "_authToken") {
		t.Fatal("the plan exposes .npmrc")
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	commit(t, tree.Root)

	// 設定が足りないだけでは、採用版を上げた利用先の必須チェックを落とさない。
	report, err := tree.Check()
	if err != nil {
		t.Fatalf("missing settings failed the check: %v", err)
	}
	found := dependencyStatuses(t, report)
	if found["cooldown"].Status != "warn" || found["install-scripts"].Status != "warn" || found["source"].Status != "pass" {
		t.Fatalf("unexpected statuses: %+v", found)
	}
	for _, want := range []string{".npmrc: min-release-age", "web/pnpm-workspace.yaml: minimumReleaseAge", "api/pyproject.toml: tool.uv.exclude-newer"} {
		if !strings.Contains(found["cooldown"].Evidence, want) {
			t.Errorf("cooldown evidence lacks %q: %s", want, found["cooldown"].Evidence)
		}
	}
	if strings.Contains(string(JSON(report)), "_authToken") {
		t.Fatal("the report exposes .npmrc")
	}

	// 利用先が自分の設定を足しても、生成物との差分や衝突にならない。
	write(t, tree.Root, ".npmrc", secret+"\nmin-release-age=7\nignore-scripts=true\n")
	write(t, tree.Root, "web/package.json", `{"name":"web","packageManager":"pnpm@11.20.0"}`)
	write(t, tree.Root, "api/pyproject.toml", "[project]\nname = 'api'\n\n[tool.uv]\nexclude-newer = \"7 days\"\n")
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("settings changed the plan: %+v %v", again, err)
	}
	report, err = tree.Check()
	if err != nil {
		t.Fatal(err)
	}
	for check, result := range dependencyStatuses(t, report) {
		if result.Status != "pass" {
			t.Errorf("%s: %+v", check, result)
		}
	}

	// 取得元の差し替えは不適合として終了コード 1 にし、ほかの判定は報告に残す。
	write(t, tree.Root, "package-lock.json", strings.Replace(npmLockOK, "https://registry.npmjs.org/a/-/a-1.0.0.tgz", "https://example.com/a-1.0.0.tgz", 1))
	report, err = tree.Check()
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), lockfileSourceRule) {
		t.Fatalf("replaced source was accepted: %v", err)
	}
	found = dependencyStatuses(t, report)
	if found["source"].Status != "fail" || !strings.Contains(found["source"].Evidence, "package-lock.json: 1 packages") || !strings.Contains(found["source"].Evidence, "node_modules/a") || found["cooldown"].Status != "pass" {
		t.Fatalf("unexpected statuses: %+v", found)
	}
	if _, err := tree.Verify(nil); ExitCode(err) != 1 {
		t.Fatalf("verify ran project tasks after a failed check: %v", err)
	}

	// 読めない .npmrc は check 全体の失敗にせず、未設定として報告する。
	write(t, tree.Root, "package-lock.json", npmLockOK)
	write(t, tree.Root, ".npmrc", "min-release-age=7\n\x00")
	report, err = tree.Check()
	if err != nil {
		t.Fatal(err)
	}
	if found = dependencyStatuses(t, report); found["cooldown"].Status != "warn" || !strings.Contains(found["cooldown"].Evidence, ".npmrc: cannot be read") {
		t.Fatalf("unexpected statuses: %+v", found)
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
	results := dependencyResults(nil, nil, "main")
	if len(results) != len(declared) {
		t.Fatalf("declared %v, reported %+v", declared, results)
	}
	for _, r := range results {
		if !declared[r.Rule+"/"+r.Check] {
			t.Errorf("undeclared check: %s/%s", r.Rule, r.Check)
		}
	}
}
