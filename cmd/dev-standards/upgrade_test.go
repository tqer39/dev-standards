package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tqer39/dev-standards/internal/engine"
	"go.yaml.in/yaml/v3"
)

type upgradeFixture struct {
	t    *testing.T
	root string
}

func newUpgradeFixture(t *testing.T) upgradeFixture {
	t.Helper()
	f := upgradeFixture{t, t.TempDir()}
	if b, err := exec.Command("git", "-C", f.root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git: %s: %v", b, err)
	}
	for name, content := range map[string]string{
		"package.json":      `{"name":"sample"}`,
		"package-lock.json": `{"lockfileVersion":3}`,
		"decision.md":       strings.ReplaceAll(languageRecord, "Go", "JavaScript"),
		"mise.toml": "[tools]\nnode='24.21.0'\n" +
			"[tasks.\"repo:bootstrap\"]\nrun='echo bootstrap'\n" +
			"[tasks.\"repo:lint\"]\nrun='echo lint'\n" +
			"[tasks.\"repo:format\"]\nrun='echo format'\n" +
			"[tasks.\"repo:format:check\"]\nrun='echo format-check'\n" +
			"[tasks.\"repo:test\"]\nrun='echo test'\n" +
			"[tasks.\"repo:build\"]\nrun='echo build'\n",
	} {
		f.write(name, []byte(content))
	}
	plan := filepath.Join(t.TempDir(), "adopt.json")
	f.invoke(1, "adopt", "--profile", "mise-project", "--decision", "decision.md", "--output", plan)
	f.invoke(0, "apply", "--plan", plan)
	return f
}

func (f upgradeFixture) invoke(want int, args ...string) []byte {
	f.t.Helper()
	out, _ := f.invokeWithError(want, args...)
	return out
}

func (f upgradeFixture) invokeWithError(want int, args ...string) ([]byte, string) {
	f.t.Helper()
	var out, log bytes.Buffer
	args = append(args, "--repo", f.root)
	if got := run(args, &out, &log); got != want {
		f.t.Fatalf("%v: exit %d want %d\n%s\n%s", args, got, want, &out, &log)
	}
	return out.Bytes(), log.String()
}

func (f upgradeFixture) reject(message string, args ...string) {
	f.t.Helper()
	_, log := f.invokeWithError(2, args...)
	if !strings.Contains(log, message) {
		f.t.Fatalf("想定した拒否理由 %q がありません: %s", message, log)
	}
}

func (f upgradeFixture) read(name string) []byte {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f upgradeFixture) write(name string, content []byte) {
	f.t.Helper()
	p := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0644); err != nil {
		f.t.Fatal(err)
	}
}

func (f upgradeFixture) changeNode(version string) {
	f.write(".standards/mise.project.toml", bytes.ReplaceAll(
		f.read(".standards/mise.project.toml"), []byte("24.21.0"), []byte(version)))
}

func TestExplicitUpgradePlanKeepsManualAdoptionAndIsIdempotent(t *testing.T) {
	f := newUpgradeFixture(t)
	lock, generated := f.read(".standards/lock.json"), f.read("mise.toml")
	f.changeNode("24.22.2")
	f.reject("lock does not match this CLI bundle", "plan")
	path := filepath.Join(t.TempDir(), "upgrade.json")
	b := f.invoke(1, "plan", "--upgrade", "--output", path)
	var plan engine.Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Request.Mode != "upgrade" || plan.Request.Automation {
		t.Fatalf("明示的な手動更新ではない計画: %+v", plan.Request)
	}
	for _, change := range plan.Changes {
		switch change.Path {
		case "mise.toml", ".standards/lock.json", ".standards/state.json":
		default:
			t.Fatalf("ツール更新で無関係なファイルを変更: %s", change.Path)
		}
	}
	if !bytes.Equal(lock, f.read(".standards/lock.json")) || !bytes.Equal(generated, f.read("mise.toml")) {
		t.Fatal("計画の作成だけで設定が変化した")
	}
	f.invoke(0, "apply", "--plan", path)
	f.invoke(0, "check")
	f.invoke(0, "plan")
	f.invoke(0, "plan", "--upgrade")
	for _, name := range []string{".codex/hooks.json", ".github/workflows/dev-standards.yml"} {
		if _, err := os.Stat(filepath.Join(f.root, name)); !os.IsNotExist(err) {
			t.Fatalf("自律保守の設定が追加された: %s", name)
		}
	}
}

func TestExplicitUpgradePreservesConflictAndStalePlanProtection(t *testing.T) {
	for _, scenario := range []string{"生成物の編集", "計画後の入力変更"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUpgradeFixture(t)
			lock := f.read(".standards/lock.json")
			f.changeNode("24.22.2")
			path := filepath.Join(t.TempDir(), "upgrade.json")
			if scenario == "生成物の編集" {
				f.write("mise.toml", append(f.read("mise.toml"), []byte("\n# ローカルの編集\n")...))
				f.invoke(2, "plan", "--upgrade", "--output", path)
			} else {
				f.invoke(1, "plan", "--upgrade", "--output", path)
				f.write(".standards/mise.project.toml", append(f.read(".standards/mise.project.toml"), []byte("\n# 計画後の編集\n")...))
			}
			before := f.read("mise.toml")
			f.invoke(2, "apply", "--plan", path)
			if !bytes.Equal(before, f.read("mise.toml")) || !bytes.Equal(lock, f.read(".standards/lock.json")) {
				t.Fatal("拒否した計画が設定を変更した")
			}
		})
	}
}

func TestExplicitUpgradeUsesRequestedCLIInsteadOfCachedRuntime(t *testing.T) {
	// 保守用の子プロセスでも、通常開発時の委譲経路を明示的に検証する。
	t.Setenv("DEV_STANDARDS_MAINTENANCE", "")
	f := newUpgradeFixture(t)
	var lock engine.Lock
	if err := json.Unmarshal(f.read(".standards/lock.json"), &lock); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	name := "dev-standards"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(state, "bundles", lock.Bundle, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	// 古い起動先を呼んだ場合は必ず失敗するようにする。
	if err := os.WriteFile(p, []byte("実行ファイルではありません"), 0755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"state": state})
	f.write(".standards/cache/host.json", b)
	// 設定が正常な時点では、委譲されない通常の plan は成功するはずである。
	f.invoke(2, "plan")
	f.changeNode("24.22.2")
	f.invoke(1, "plan", "--upgrade")
}

func TestExplicitUpgradeRejectsUntrustedLockAndDowngrade(t *testing.T) {
	for _, scenario := range []string{"不正な正本リポジトリ", "古い CLI への更新", "新しいロックからの更新"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUpgradeFixture(t)
			reason := "untrusted previous lock"
			if scenario == "不正な正本リポジトリ" {
				f.write(".standards/lock.json", bytes.ReplaceAll(
					f.read(".standards/lock.json"), []byte("tqer39/dev-standards"), []byte("example/untrusted")))
			} else if scenario == "古い CLI への更新" {
				reason = "automatic downgrades are forbidden"
				var manifest engine.Manifest
				if err := json.Unmarshal(f.read(".standards/repository.yml"), &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.Standard.Version = "99.0.0"
				f.write(".standards/repository.yml", engine.JSON(manifest))
			} else {
				var lock engine.Lock
				if err := json.Unmarshal(f.read(".standards/lock.json"), &lock); err != nil {
					t.Fatal(err)
				}
				lock.Version = "99.0.0"
				f.write(".standards/lock.json", engine.JSON(lock))
			}
			lock, state := f.read(".standards/lock.json"), f.read(".standards/state.json")
			f.reject(reason, "plan", "--upgrade")
			if !bytes.Equal(lock, f.read(".standards/lock.json")) || !bytes.Equal(state, f.read(".standards/state.json")) {
				t.Fatal("拒否した更新計画がロックや状態を変更した")
			}
		})
	}
}

func TestExplicitUpgradeShowsManifestNormalizationBeforeApplying(t *testing.T) {
	f := newUpgradeFixture(t)
	var manifest map[string]any
	if err := json.Unmarshal(f.read(".standards/repository.yml"), &manifest); err != nil {
		t.Fatal(err)
	}
	b, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.write(".standards/repository.yml", append([]byte("# 手動の YAML コメント\n"), b...))
	before := f.read(".standards/repository.yml")
	path := filepath.Join(t.TempDir(), "upgrade.json")
	var plan engine.Plan
	if err := json.Unmarshal(f.invoke(1, "plan", "--upgrade", "--output", path), &plan); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range plan.Changes {
		if change.Path == ".standards/repository.yml" {
			found = bytes.Equal([]byte(change.Before.Content), before) && json.Valid([]byte(change.After.Content))
		}
	}
	if !found || !bytes.Equal(before, f.read(".standards/repository.yml")) {
		t.Fatal("宣言の形式変更が適用前に正しく提示されない")
	}
	f.invoke(0, "apply", "--plan", path)
	f.invoke(0, "check")
	f.invoke(0, "plan", "--upgrade")
}
