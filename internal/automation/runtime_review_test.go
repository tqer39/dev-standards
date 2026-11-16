package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	bundle "github.com/tqer39/dev-standards"
	"github.com/tqer39/dev-standards/internal/engine"
)

func launcherReviewRepository(t *testing.T, id runtimeIdentity) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "--quiet", "-b", "main")
	put(t, repo, ".gitignore", "/.standards/cache/\n")
	put(t, repo, ".standards/lock.json", string(mustJSON(t, engine.Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: id.Version, Bundle: id.Bundle})))
	runGit(t, repo, "add", ".gitignore", ".standards/lock.json")
	runGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "検証用リポジトリ")
	return repo
}

func TestLauncherRefreshRetainsMissingLockAndRescuesAfterRestore(t *testing.T) {
	old, newer := buildRuntime(t, "0.0.1"), buildRuntime(t, "0.0.2")
	oldID, newID := runtimeIdentity{"0.0.1", bundle.Digest()}, runtimeIdentity{"0.0.2", bundle.Digest()}
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprintf("linked=%t", linked), func(t *testing.T) {
			state := t.TempDir()
			legacy := filepath.Join(state, "bundles", oldID.Bundle, executableName())
			t.Cleanup(func() {
				os.Chmod(filepath.Dir(legacy), 0700)
				os.Chmod(legacy, 0700)
			})
			repo := launcherReviewRepository(t, newID)
			if err := InstallRuntime(repo, newer, state); err != nil {
				t.Fatal(err)
			}
			missing := repo
			if linked {
				missing = filepath.Join(t.TempDir(), "missing")
				runGit(t, repo, "worktree", "add", "--quiet", "-b", "missing", missing)
			}
			put(t, missing, ".standards/lock.json", string(mustJSON(t, engine.Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: oldID.Version, Bundle: oldID.Bundle})))
			lock, err := os.ReadFile(filepath.Join(missing, ".standards/lock.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := InstallRuntime(missing, old, state); err != nil {
				t.Fatal(err)
			}
			launcher := filepath.Join(missing, ".standards/cache", executableName())
			before, err := os.ReadFile(launcher)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(missing, ".standards/lock.json")); err != nil {
				t.Fatal(err)
			}
			tail := filepath.Join(t.TempDir(), "tail")
			runGit(t, repo, "worktree", "add", "--quiet", "-b", "tail", tail)
			if err := InstallRuntime(tail, old, state); err != nil {
				t.Fatal(err)
			}
			entry := Entry{Path: repo}
			err = refreshLaunchers(context.Background(), Exec{}, entry, newer, state)
			if (linked && err != nil) || (!linked && (err == nil || !strings.Contains(err.Error(), "applied identity is missing"))) {
				t.Fatalf("採用版が不明な起動口の扱い: %v", err)
			}
			after, err := os.ReadFile(launcher)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("ロック欠落中に唯一の旧ランチャーを変更しました: %v", err)
			}
			if _, err := os.Stat(oldID.path(state)); !os.IsNotExist(err) {
				t.Fatal("採用版が不明な状態で実行環境を推定して公開しました")
			}
			if _, err := os.Stat(legacy); !os.IsNotExist(err) {
				t.Fatal("採用版が不明な状態で旧形式スロットを作りました")
			}
			if linked {
				files, err := filepath.Glob(filepath.Join(repo, ".standards/cache/feedback/*.json"))
				if err != nil || len(files) != 1 {
					t.Fatalf("ロック欠落の観測を記録していません: %v %v", files, err)
				}
				data, err := os.ReadFile(files[0])
				if err != nil || !bytes.Contains(data, []byte("applied identity is missing")) {
					t.Fatalf("採用版不明の診断が失われました: %s %v", data, err)
				}
				if actual, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(tail, ".standards/cache", executableName())); err != nil || actual != newID {
					t.Fatalf("一致する採用版の空の転送先を欠落と誤認しました: %+v %v", actual, err)
				}
			}
			put(t, missing, ".standards/lock.json", string(lock))
			if err := refreshLaunchers(context.Background(), Exec{}, entry, newer, state); err != nil {
				t.Fatalf("ロック復元後の退避・移行: %v", err)
			}
			if actual, err := inspectRuntime(context.Background(), Exec{}, oldID.path(state)); err != nil || actual != oldID {
				t.Fatalf("保持した旧ランチャーを退避できません: %+v %v", actual, err)
			}
			if actual, err := inspectRuntime(context.Background(), Exec{}, launcher); err != nil || actual != newID {
				t.Fatalf("復元後の起動口を更新できません: %+v %v", actual, err)
			}
			if err := refreshLaunchers(context.Background(), Exec{}, entry, newer, state); err != nil {
				t.Fatalf("再適用: %v", err)
			}
			// ソース自身には採用ロックがなくても、検証済みの起動口を配置できる。
			put(t, repo, "go.mod", "module github.com/tqer39/dev-standards\n")
			if err := os.Remove(filepath.Join(repo, ".standards/lock.json")); err != nil {
				t.Fatal(err)
			}
			if err := InstallRuntime(repo, old, state); err != nil {
				t.Fatal(err)
			}
			if err := refreshLaunchers(context.Background(), Exec{}, entry, newer, state); err != nil {
				t.Fatal(err)
			}
			if actual, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(repo, ".standards/cache", executableName())); err != nil || actual != newID {
				t.Fatalf("ソースの例外を失いました: %+v %v", actual, err)
			}
		})
	}
}

func TestDeferredLauncherObservationBoundsUTF8(t *testing.T) {
	for _, message := range []string{"短い観測", strings.Repeat("a", 2048), strings.Repeat("a", 2047) + "界", strings.Repeat("あ", 1000)} {
		got := boundObservation(message)
		if len(got) > 2048 || !utf8.ValidString(got) || !strings.HasPrefix(message, got) || (len(message) <= 2048 && got != message) {
			t.Fatalf("観測の切り詰めが不正です: 入力=%d 出力=%d", len(message), len(got))
		}
	}
	repo := launcherReviewRepository(t, runtimeIdentity{bundle.Version, bundle.Digest()})
	if err := deferLinkedLauncher(Entry{Path: repo}, strings.Repeat("長い作業場所", 500), fmt.Errorf("移行保留")); err != nil {
		t.Fatalf("長い補助 worktree の観測で停止しました: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(repo, ".standards/cache/feedback/*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("観測の保存: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var observation map[string]string
	if err := json.Unmarshal(data, &observation); err != nil || len(observation["observation"]) > 2048 || !utf8.ValidString(observation["observation"]) {
		t.Fatalf("保存した観測が不正です: %v", err)
	}
	path := strings.Repeat("a", feedbackObservationLimit-len("linked worktree ")-5) + "sk-" + strings.Repeat("x", 30)
	if err := deferLinkedLauncher(Entry{Path: repo}, path, fmt.Errorf("移行保留")); err == nil || !strings.Contains(err.Error(), "secret-like") {
		t.Fatalf("切り詰めの境界で秘密情報の検査をすり抜けました: %v", err)
	}
	if after, err := filepath.Glob(filepath.Join(repo, ".standards/cache/feedback/*.json")); err != nil || len(after) != len(files) {
		t.Fatal("拒否した秘密情報らしい観測を保存しました")
	}
}

type orderedLauncherWorktrees []string

func (paths orderedLauncherWorktrees) Run(ctx context.Context, dir, binary string, args []string, input string) (string, error) {
	if binary == "git" && strings.Contains(strings.Join(args, " "), "worktree list") {
		var output string
		for _, path := range paths {
			output += "worktree " + path + "\x00\x00"
		}
		return output, nil
	}
	return (Exec{}).Run(ctx, dir, binary, args, input)
}

func TestLauncherRefreshReportsFeedbackFailureAfterRemainingWorktrees(t *testing.T) {
	old, newer := buildRuntime(t, "0.0.1"), buildRuntime(t, "0.0.2")
	id := runtimeIdentity{"0.0.2", bundle.Digest()}
	repo, stale, missing, tail, state := launcherReviewRepository(t, id), launcherReviewRepository(t, id), launcherReviewRepository(t, id), launcherReviewRepository(t, id), t.TempDir()
	for _, path := range []string{repo, stale, missing, tail} {
		if err := InstallRuntime(path, old, state); err != nil {
			t.Fatal(err)
		}
	}
	put(t, stale, ".standards/lock.json", string(mustJSON(t, engine.Lock{SchemaVersion: 1, Repository: bundle.Repository, Version: "77.0.0", Bundle: id.Bundle})))
	if err := os.Remove(filepath.Join(missing, ".standards/lock.json")); err != nil {
		t.Fatal(err)
	}
	put(t, repo, ".standards/cache/feedback", "記録先の障害")
	err := refreshLaunchers(context.Background(), orderedLauncherWorktrees{repo, stale, missing, tail}, Entry{Path: repo}, newer, state)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || filepath.Base(pathErr.Path) != "feedback" {
		t.Fatalf("観測を書き込めなかった診断を失いました: %v", err)
	}
	if joined, ok := err.(interface{ Unwrap() []error }); !ok || len(joined.Unwrap()) != 2 {
		t.Fatalf("複数の観測の保存失敗を集約していません: %v", err)
	}
	if actual, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(tail, ".standards/cache", executableName())); err != nil || actual != id {
		t.Fatalf("観測の保存失敗で後続の起動口を更新できません: %+v %v", actual, err)
	}
	if actual, err := inspectRuntime(context.Background(), Exec{}, filepath.Join(stale, ".standards/cache", executableName())); err != nil || actual.Version != "0.0.1" {
		t.Fatalf("保留対象の起動口を変更しました: %+v %v", actual, err)
	}
}
