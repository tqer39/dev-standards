package automation

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bundle "github.com/tqer39/dev-standards"
	"github.com/tqer39/dev-standards/internal/engine"
)

func buildRuntime(t *testing.T, version string, legacy ...bool) string {
	t.Helper()
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := os.ReadFile(filepath.Join(source, "assets.go"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	put(t, dir, "assets.go", strings.Replace(string(assets), `const Version = "`+bundle.Version+`"`, `const Version = "`+version+`"`, 1))
	replacements := map[string]string{filepath.Join(source, "assets.go"): filepath.Join(dir, "assets.go")}
	if len(legacy) != 0 && legacy[0] {
		body, err := os.ReadFile(filepath.Join(source, "internal/automation/session.go"))
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(string(body), "func matchingRuntime(")
		end := strings.Index(string(body)[start:], "\nfunc matchingRuntimeFor") + start
		oldSelector := `func matchingRuntime(ctx context.Context, repo string) (string, error) {
			_ = bundle.Version
			state := hostState(repo)
			if state == "" { return "", nil }
			data, err := os.ReadFile(filepath.Join(repo, ".standards/lock.json"))
			var lock struct { BundleSha256 string }
			if err == nil {
				if err := json.Unmarshal(data, &lock); err != nil { return "", err }
			} else if isSource(repo) {
				data, err = os.ReadFile(filepath.Join(state, "latest.json"))
				if err != nil { return "", nil }
				var ledger Ledger
				if err := json.Unmarshal(data, &ledger); err != nil { return "", err }
				lock.BundleSha256 = ledger.Bundle
				if lock.BundleSha256 == "" { return "", nil }
			} else { return "", nil }
			if !regexp.MustCompile("^[0-9a-f]{64}$").MatchString(lock.BundleSha256) { return "", fmt.Errorf("invalid locked bundle") }
			p := filepath.Join(state, "bundles", lock.BundleSha256, executableName())
			info, err := os.Stat(p)
			if os.IsNotExist(err) { return "", nil }
			if err != nil { return "", err }
			self, _ := os.Executable()
			selfInfo, err := os.Stat(self)
			if err == nil && os.SameFile(info, selfInfo) { return "", nil }
			return p, nil
		}`
		put(t, dir, "session.go", string(body[:start])+oldSelector+string(body[end:]))
		replacements[filepath.Join(source, "internal/automation/session.go")] = filepath.Join(dir, "session.go")
		command, err := os.ReadFile(filepath.Join(source, "cmd/dev-standards/automation.go"))
		if err != nil {
			t.Fatal(err)
		}
		put(t, dir, "automation.go", strings.Replace(string(command),
			`if command == "finish" || command == "session-verify" {`,
			`if command == "prepare" || command == "finish" || command == "hook" || command == "session-verify" {`, 1))
		replacements[filepath.Join(source, "cmd/dev-standards/automation.go")] = filepath.Join(dir, "automation.go")
	}
	put(t, dir, "overlay.json", string(mustJSON(t, map[string]any{"Replace": replacements})))
	binary := filepath.Join(dir, executableName())
	cmd := exec.Command("go", "build", "-overlay", filepath.Join(dir, "overlay.json"), "-o", binary, "./cmd/dev-standards")
	cmd.Dir = source
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("テスト用 CLI のビルド: %s: %v", output, err)
	}
	return binary
}

func runtimeCommand(t *testing.T, binary, repo, input string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append(args, "--repo", repo)...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("実行環境の転送が終端しません: %v: %s", args, output)
	}
	return string(output), err
}

func TestVersionedRuntimesKeepSameBundleAndRouteEveryEntryPoint(t *testing.T) {
	// 親の保守テストから呼ばれても、ここでは検査を迂回しない通常経路を検証する。
	t.Setenv("DEV_STANDARDS_MAINTENANCE", "")
	old, newer := buildRuntime(t, "0.0.1"), buildRuntime(t, "0.0.2")
	legacyBinary := buildRuntime(t, "0.0.1", true)
	ctx := context.Background()
	state := t.TempDir()
	oldID, newID := runtimeIdentity{"0.0.1", bundle.Digest()}, runtimeIdentity{"0.0.2", bundle.Digest()}
	first, err := publishRuntime(ctx, Exec{}, old, state, oldID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publishRuntime(ctx, Exec{}, newer, state, newID)
	if err != nil || first == second {
		t.Fatalf("版が異なる CLI が同じ保存先です: %s %s %v", first, second, err)
	}
	if again, err := publishRuntime(ctx, Exec{}, old, state, oldID); err != nil || again != first {
		t.Fatalf("再公開: %s %v", again, err)
	}
	after, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("新しい版の公開が既存の実行環境を上書きしました")
	}
	// 旧形式のスロットを残しても、新形式の実行環境自身はそこへ戻らない。
	legacy := filepath.Join(state, "bundles", bundle.Digest(), executableName())
	t.Cleanup(func() {
		os.Chmod(filepath.Dir(legacy), 0700)
		os.Chmod(legacy, 0700)
	})
	if err := copyExecutable(legacyBinary, legacy); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ version, adopted, launcher string }{
		{"0.0.1", old, newer}, {"0.0.2", newer, old},
	} {
		t.Run(tc.version, func(t *testing.T) {
			repo, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			config := "[tools]\nnode=\"24.21.0\"\n"
			runGit(t, repo, "init", "--quiet", "-b", "main")
			for _, task := range []string{"bootstrap", "lint", "format", "format:check", "test", "build"} {
				config += fmt.Sprintf("[tasks.\"repo:%s\"]\nrun = 'echo verified'\n", task)
			}
			put(t, repo, "mise.toml", config)
			put(t, repo, "package.json", `{"name":"fixture"}`)
			put(t, repo, "package-lock.json", `{"lockfileVersion":3}`)
			put(t, repo, "decision.md", "# 言語選定\n\n## 要件\n\n既存のブラウザー UI を維持する。\n\n## 候補\n\nJavaScript と Go を比較する。\n\n## 選定理由\n\nJavaScript でブラウザー UI を維持する。\n")
			if output, err := runtimeCommand(t, tc.adopted, repo, "", "sync"); err == nil {
				t.Fatalf("初回導入なしに sync が成功しました: %s", output)
			}
			plan := filepath.Join(t.TempDir(), "adopt.json")
			runtimeCommand(t, tc.adopted, repo, "", "adopt", "--profile", "mise-project", "--decision", "decision.md", "--output", plan)
			if output, err := runtimeCommand(t, tc.adopted, repo, "", "apply", "--plan", plan); err != nil {
				t.Fatalf("初回適用: %s: %v", output, err)
			}
			put(t, repo, ".standards/cache/host.json", string(mustJSON(t, map[string]string{"state": state})))
			t.Setenv("MISE_TRUSTED_CONFIG_PATHS", repo)
			t.Setenv("PATH", filepath.Dir(tc.launcher)+string(os.PathListSeparator)+os.Getenv("PATH"))
			for _, args := range [][]string{{"prepare"}, {"plan"}, {"session-verify"}, {"finish"}} {
				if output, err := runtimeCommand(t, tc.launcher, repo, "", args...); err != nil {
					t.Fatalf("%v: %s: %v", args, output, err)
				}
			}
			for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
				output, err := runtimeCommand(t, tc.launcher, repo, fmt.Sprintf(`{"hook_event_name":%q}`, event), "hook")
				if err != nil || strings.Contains(output, `"decision": "block"`) || strings.Contains(output, "診断:") {
					t.Fatalf("hook %s: %s: %v", event, output, err)
				}
			}
			put(t, repo, "unverified.txt", "未検証の変更")
			if output, err := runtimeCommand(t, tc.launcher, repo, "", "finish"); err == nil || !strings.Contains(output, "検証記録がありません") {
				t.Fatalf("検証失敗を成功扱いしました: %s: %v", output, err)
			}
			if tc.version == oldID.Version {
				if err := os.Remove(filepath.Join(repo, "unverified.txt")); err != nil {
					t.Fatal(err)
				}
				exerciseRuntimeAdvance(t, repo, state, old, newer, legacyBinary)
			}
		})
	}
	t.Run("source", func(t *testing.T) {
		repo := t.TempDir()
		runGit(t, repo, "init", "--quiet")
		put(t, repo, "go.mod", "module github.com/tqer39/dev-standards\n")
		put(t, repo, "mise.toml", "[tasks.check]\nrun = 'echo source-verified'\n")
		put(t, repo, ".standards/cache/host.json", string(mustJSON(t, map[string]string{"state": state})))
		t.Setenv("MISE_TRUSTED_CONFIG_PATHS", repo)
		if output, err := runtimeCommand(t, newer, repo, "", "prepare"); err != nil || !strings.Contains(output, "自体の開発") {
			t.Fatalf("ソースの準備: %s: %v", output, err)
		}
		for _, command := range []string{"session-verify", "finish"} {
			if output, err := runtimeCommand(t, newer, repo, "", command); err != nil {
				t.Fatalf("ソースの %s: %s: %v", command, output, err)
			}
		}
	})
	t.Run("missing-runtime-hooks", func(t *testing.T) {
		repo := t.TempDir()
		runGit(t, repo, "init", "--quiet")
		put(t, repo, ".standards/cache/host.json", string(mustJSON(t, map[string]string{"state": state})))
		put(t, repo, ".standards/lock.json", string(mustJSON(t, engine.Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: "99.0.0", Bundle: bundle.Digest()})))
		for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
			output, err := runtimeCommand(t, newer, repo, fmt.Sprintf(`{"hook_event_name":%q}`, event), "hook")
			if err != nil || !strings.Contains(output, "診断:") || !strings.Contains(output, "unavailable") {
				t.Fatalf("hook の診断が失われました: %s %v", output, err)
			}
		}
		for _, active := range []bool{false, true} {
			output, err := runtimeCommand(t, newer, repo, fmt.Sprintf(`{"hook_event_name":"Stop","stop_hook_active":%t}`, active), "hook")
			blocked := strings.Contains(output, `"decision": "block"`)
			if err != nil || blocked == active || (active && !strings.Contains(output, "検証未完了")) {
				t.Fatalf("Stop の再試行制御が失われました: active=%t %s %v", active, output, err)
			}
		}
	})
}

func exerciseRuntimeAdvance(t *testing.T, repo, state, old, newer, legacy string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	bare(t, remote)
	repository(t, repo, remote)
	before := runGit(t, repo, "rev-parse", "HEAD")
	linked := filepath.Join(root, "linked")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "linked-old", linked, before)
	for _, path := range []string{repo, linked} {
		if err := InstallRuntime(path, legacy, state); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(root, "other-host")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "other-host", other, before)
	if err := InstallRuntime(other, legacy, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "stale")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "stale", stale, before)
	if err := InstallRuntime(stale, legacy, state); err != nil {
		t.Fatal(err)
	}
	put(t, stale, ".standards/lock.json", string(mustJSON(t, engine.Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: "77.0.0", Bundle: bundle.Digest()})))
	busy := filepath.Join(root, "busy")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "busy", busy, before)
	if err := InstallRuntime(busy, legacy, state); err != nil {
		t.Fatal(err)
	}
	put(t, busy, filepath.Join(".standards/cache", executableName()+".new"), "作業中のコピー")
	put(t, linked, "user-work.txt", "作業を保持する")
	launcher := filepath.Join(repo, ".standards/cache", executableName())
	if output, err := runtimeCommand(t, launcher, repo, "", "prepare"); err != nil || !strings.Contains(output, "適用版: 0.0.1") {
		t.Fatalf("旧形式のランチャー: %s: %v", output, err)
	}
	updater := filepath.Join(root, "updater")
	runGit(t, root, "clone", "--quiet", remote, updater)
	plan := filepath.Join(root, "upgrade.json")
	runtimeCommand(t, newer, updater, "", "plan", "--upgrade", "--output", plan)
	if output, err := runtimeCommand(t, newer, updater, "", "apply", "--plan", plan); err != nil {
		t.Fatalf("次版の適用: %s: %v", output, err)
	}
	runGit(t, updater, "add", ".standards/lock.json", ".standards/repository.yml", ".standards/state.json")
	runGit(t, updater, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "次版へ更新")
	runGit(t, updater, "push", "--quiet", "origin", "main")
	wanted := runGit(t, updater, "rev-parse", "HEAD")
	c := Default(Entry{Path: filepath.Join(root, "source"), Remote: filepath.Join(root, "source.git"), Branch: "main", Role: "source"})
	c.Projects = []Entry{{Path: repo, Remote: remote, Branch: "main", Role: "project"}}
	if err := Save(filepath.Join(state, "fleet.json"), c); err != nil {
		t.Fatal(err)
	}
	put(t, state, "latest.json", string(mustJSON(t, Ledger{Outcomes: []Outcome{{Repository: remote, Status: "updated", Commit: wanted}}})))
	// 旧ランチャーだけが残る状態から、一致する実行環境を退避して移行する。
	if err := os.Remove((runtimeIdentity{"0.0.1", bundle.Digest()}).path(state)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(state, "bundles", bundle.Digest(), executableName())); err != nil {
		t.Fatal(err)
	}
	canonicalState, err := filepath.EvalSymlinks(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshLaunchers(context.Background(), Exec{}, c.Projects[0], newer, canonicalState); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{repo, linked} {
		id, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(path, ".standards/cache", executableName()))
		if err != nil || id.Version != "0.0.2" {
			t.Fatalf("起動口の移行: %+v: %v", id, err)
		}
	}
	if id, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(other, ".standards/cache", executableName())); err != nil || id.Version != "0.0.1" {
		t.Fatalf("別の台帳を使う起動口を変更しました: %+v %v", id, err)
	}
	if id, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(stale, ".standards/cache", executableName())); err != nil || id.Version != "0.0.1" {
		t.Fatalf("移行できない補助 worktree の起動口を変更しました: %+v %v", id, err)
	}
	if id, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(busy, ".standards/cache", executableName())); err != nil || id.Version != "0.0.1" {
		t.Fatalf("コピーに失敗した補助 worktree の起動口を変更しました: %+v %v", id, err)
	}
	legacySlot := filepath.Join(state, "bundles", bundle.Digest(), executableName())
	if err := copyExecutable(newer, legacySlot); err == nil {
		t.Fatal("旧形式の保護済みスロットを別の版で上書きできました")
	}
	if id, err := inspectRuntime(context.Background(), Exec{}, legacySlot); err != nil || id.Version != "0.0.1" {
		t.Fatalf("旧形式スロットの保護で識別子が変わりました: %+v %v", id, err)
	}
	if content, err := os.ReadFile(filepath.Join(busy, ".standards/cache", executableName()+".new")); err != nil || string(content) != "作業中のコピー" {
		t.Fatal("既存の作業用コピーを削除しました")
	}
	feedback, err := filepath.Glob(filepath.Join(repo, ".standards/cache/feedback/*.json"))
	if err != nil || len(feedback) == 0 {
		t.Fatalf("補助 worktree の移行保留が記録されていません: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(linked, "user-work.txt")); err != nil || string(content) != "作業を保持する" {
		t.Fatal("作業中の worktree の内容を変更しました")
	}
	// 旧ランチャーで保持していた実際の旧形式スロットでも、版が一致すれば終端する。
	if err := os.Remove((runtimeIdentity{"0.0.1", bundle.Digest()}).path(state)); err != nil {
		t.Fatal(err)
	}
	if output, err := runtimeCommand(t, filepath.Join(linked, ".standards/cache", executableName()), linked, "", "prepare"); err != nil || !strings.Contains(output, "適用版: 0.0.1") {
		t.Fatalf("移行後の旧版ブランチ: %s: %v", output, err)
	}
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", repo+string(os.PathListSeparator)+linked)
	for _, command := range []string{"session-verify", "finish"} {
		if output, err := runtimeCommand(t, filepath.Join(linked, ".standards/cache", executableName()), linked, "", command); err != nil {
			t.Fatalf("旧形式の実行環境への転送: %s %s %v", command, output, err)
		}
	}
	put(t, repo, "dirty.txt", "作業中")
	if output, err := runtimeCommand(t, old, repo, "", "prepare"); err != nil || !strings.Contains(output, "適用版: 0.0.1") {
		t.Fatalf("dirty な main の保持: %s: %v", output, err)
	}
	if err := os.Remove(filepath.Join(repo, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "checkout", "-qb", "old-work")
	if output, err := runtimeCommand(t, newer, repo, "", "prepare"); err != nil || !strings.Contains(output, "適用版: 0.0.1") {
		t.Fatalf("旧版の作業ブランチ: %s: %v", output, err)
	}
	if runGit(t, repo, "rev-parse", "HEAD") != before {
		t.Fatal("旧版の作業ブランチを更新しました")
	}
	runGit(t, repo, "checkout", "-q", "main")
	if output, err := runtimeCommand(t, launcher, repo, "", "prepare"); err != nil || !strings.Contains(output, "適用版: 0.0.2") {
		t.Fatalf("main の更新後に新しい版へ到達しません: %s: %v", output, err)
	}
	if runGit(t, repo, "rev-parse", "HEAD") != wanted {
		t.Fatal("clean な main が更新されていません")
	}
	for _, command := range []string{"session-verify", "finish"} {
		if output, err := runtimeCommand(t, old, repo, "", command); err != nil {
			t.Fatalf("main の更新後に終端しません: %s: %s: %v", command, output, err)
		}
	}
}

func TestRuntimeSelectionRejectsWrongIdentityAndKeepsLegacyFallback(t *testing.T) {
	t.Setenv("DEV_STANDARDS_MAINTENANCE", "")
	binary := buildRuntime(t, "0.0.1")
	state, repo := t.TempDir(), t.TempDir()
	runGit(t, repo, "init", "--quiet")
	put(t, repo, ".standards/cache/host.json", string(mustJSON(t, map[string]string{"state": state})))
	id := runtimeIdentity{"0.0.1", bundle.Digest()}
	lock := engine.Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: id.Version, Bundle: id.Bundle}
	put(t, repo, ".standards/lock.json", string(mustJSON(t, lock)))
	legacy := filepath.Join(state, "bundles", id.Bundle, executableName())
	t.Cleanup(func() {
		os.Chmod(filepath.Dir(legacy), 0700)
		os.Chmod(legacy, 0700)
	})
	if err := copyExecutable(binary, legacy); err != nil {
		t.Fatal(err)
	}
	if got, err := MatchingRuntime(repo); err != nil || got != legacy {
		t.Fatalf("旧形式キャッシュを選択できません: %s %v", got, err)
	}
	// 以降の衝突ケースを作るため、テストで意図的に旧スロットの保護を解除する。
	if err := os.Chmod(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	lock.Version = "0.0.2"
	put(t, repo, ".standards/lock.json", string(mustJSON(t, lock)))
	if _, err := MatchingRuntime(repo); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("同じバンドルの別の版を選択しました: %v", err)
	}
	wrong := runtimeIdentity{lock.Version, lock.Bundle}
	if err := copyExecutable(binary, wrong.path(state)); err != nil {
		t.Fatal(err)
	}
	if _, err := MatchingRuntime(repo); err == nil || !strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("新形式の識別子衝突を見逃しました: %v", err)
	}
	if _, err := publishRuntime(context.Background(), Exec{}, binary, state, wrong); err == nil {
		t.Fatal("衝突する実行環境を再公開できました")
	}
	missing := runtimeIdentity{"0.0.3", id.Bundle}
	if _, err := publishRuntime(context.Background(), Exec{}, filepath.Join(t.TempDir(), "missing"), state, missing); err == nil {
		t.Fatal("失敗したコピーを成功扱いしました")
	}
	if _, err := os.Stat(missing.path(state)); !os.IsNotExist(err) {
		t.Fatal("失敗したコピーが公開されています")
	}
	if _, err := publishRuntime(context.Background(), Exec{}, binary, state, missing); err == nil {
		t.Fatal("コピーした実行ファイルの識別子不一致を見逃しました")
	}
	if _, err := os.Stat(missing.path(state)); !os.IsNotExist(err) {
		t.Fatal("識別子の異なるコピーが公開されています")
	}
	lock.Version = id.Version
	put(t, repo, ".standards/lock.json", string(mustJSON(t, lock)))
	if err := InstallRuntime(repo, binary, state); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	newer := buildRuntime(t, "0.0.2")
	if err := copyExecutable(newer, legacy); err != nil {
		t.Fatal(err)
	}
	if err := refreshLaunchers(context.Background(), Exec{}, Entry{Path: repo}, newer, state); err == nil || !strings.Contains(err.Error(), "would lose") {
		t.Fatalf("旧版の実行環境を失う起動口更新を拒否しませんでした: %v", err)
	}
	if actual, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(repo, ".standards/cache", executableName())); err != nil || actual != id {
		t.Fatalf("拒否した起動口更新が元の実行ファイルを変更しました: %+v %v", actual, err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err := refreshLaunchers(context.Background(), Exec{}, Entry{Path: repo}, newer, state); err != nil {
		t.Fatalf("一致する旧ランチャーを退避して移行できません: %v", err)
	}
	if actual, err := inspectRuntime(context.Background(), Exec{}, id.path(state)); err != nil || actual != id {
		t.Fatalf("旧ランチャーの退避: %+v %v", actual, err)
	}
	blockedState := t.TempDir()
	put(t, blockedState, "runtimes", "ファイルを保持する")
	if _, err := publishRuntime(context.Background(), Exec{}, binary, blockedState, id); err == nil {
		t.Fatal("ディレクトリではない親を受け入れました")
	}
	if !runtimeWindows() {
		linkedState, outside := t.TempDir(), t.TempDir()
		if err := os.Symlink(outside, filepath.Join(linkedState, "runtimes")); err != nil {
			t.Fatal(err)
		}
		if _, err := publishRuntime(context.Background(), Exec{}, binary, linkedState, id); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("親のシンボリックリンクを拒否しませんでした: %v", err)
		}
		if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
			t.Fatal("共有キャッシュ外へ書き込みました")
		}
	}
	put(t, repo, "go.mod", "module github.com/tqer39/dev-standards\n")
	put(t, state, "latest.json", `{"bundle":"不正な台帳"}`)
	if got, err := MatchingRuntime(repo); err != nil || got != "" {
		t.Fatalf("ソースを公開済み実行環境へ転送しました: %s %v", got, err)
	}
	put(t, repo, "go.mod", "module example.com/fixture\n")
	put(t, repo, ".standards/lock.json", `{"version":"../../outside","bundleSha256":"`+id.Bundle+`"}`)
	if _, err := MatchingRuntime(repo); err == nil {
		t.Fatal("不正なロックを受け入れました")
	}
}
