package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/tqer39/dev-standards/internal/engine"
)

type runtimeIdentity struct {
	Version string
	Bundle  string
}

func executableName() string {
	if runtimeWindows() {
		return "dev-standards.exe"
	}
	return "dev-standards"
}

func (id runtimeIdentity) path(state string) string {
	return filepath.Join(state, "runtimes", id.Version, id.Bundle, executableName())
}

func checkRuntimeParents(state, target string) error {
	for dir := filepath.Dir(target); dir != filepath.Clean(state); dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("runtime parent must be a directory, not a symlink: %s", dir)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(dir) == dir {
			return fmt.Errorf("runtime path escapes state directory")
		}
	}
	return nil
}

func inspectRuntime(ctx context.Context, x Executor, binary string) (runtimeIdentity, error) {
	output, err := x.Run(ctx, "", binary, []string{"version"}, "")
	if err != nil {
		return runtimeIdentity{}, err
	}
	fields := strings.Fields(output)
	if len(fields) != 3 || fields[0] != "dev-standards" || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(fields[1]) || !regexp.MustCompile(`^bundle=[0-9a-f]{64}$`).MatchString(fields[2]) {
		return runtimeIdentity{}, fmt.Errorf("invalid built CLI identity")
	}
	return runtimeIdentity{fields[1], strings.TrimPrefix(fields[2], "bundle=")}, nil
}

func publishRuntime(ctx context.Context, x Executor, binary, state string, id runtimeIdentity) (string, error) {
	return publishRuntimeAt(ctx, x, binary, state, id, id.path(state))
}

func publishRuntimeAt(ctx context.Context, x Executor, binary, state string, id runtimeIdentity, destination string) (string, error) {
	if err := checkRuntimeParents(state, destination); err != nil {
		return "", err
	}
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("runtime must be a regular file: %s", destination)
		}
		actual, err := inspectRuntime(ctx, x, destination)
		if err != nil {
			return "", err
		}
		if actual != id {
			return "", fmt.Errorf("immutable runtime identity conflict: %s", destination)
		}
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	// 完成したディレクトリだけを公開し、途中で失敗したコピーを選択させない。
	parent := filepath.Dir(filepath.Dir(destination))
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".runtime-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := copyExecutable(binary, filepath.Join(tmp, executableName())); err != nil {
		return "", err
	}
	actual, err := inspectRuntime(ctx, x, filepath.Join(tmp, executableName()))
	if err != nil {
		return "", err
	}
	if actual != id {
		return "", fmt.Errorf("copied runtime identity does not match publication")
	}
	// 実行ファイルだけが削除された空ディレクトリも回復する。非空なら保持して失敗する。
	if err := os.Remove(filepath.Dir(destination)); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(tmp, filepath.Dir(destination)); err != nil {
		return "", err
	}
	return destination, nil
}

func runtimeWindows() bool { return runtime.GOOS == "windows" }
func copyExecutable(source, destination string) error {
	if filepath.Clean(source) == filepath.Clean(destination) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime must not be a symlink")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := destination + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, destination)
}
func InstallRuntime(repo, binary, state string) error {
	name := "dev-standards"
	if runtimeWindows() {
		name += ".exe"
	}
	cache := filepath.Join(repo, ".standards/cache")
	if info, err := os.Lstat(filepath.Dir(cache)); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(".standards must not be a symlink")
	}
	if info, err := os.Lstat(cache); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cache must not be a symlink")
	}
	if err := copyExecutable(binary, filepath.Join(cache, name)); err != nil {
		return err
	}
	if state != "" {
		b, _ := json.Marshal(map[string]string{"state": state})
		target := filepath.Join(cache, "host.json")
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("host locator must not be a symlink")
		}
		return os.WriteFile(target, b, 0600)
	}
	return nil
}

func refreshLaunchers(ctx context.Context, x Executor, entry Entry, binary, state string) error {
	// 旧ランチャーは新しい保存形式を知らないため、hook のパスを保ったまま移行する。
	if !sameHostState(entry.Path, state) {
		return nil
	}
	output, err := git(ctx, x, entry.Path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	current, err := inspectRuntime(ctx, x, binary)
	if err != nil {
		return err
	}
	for _, field := range strings.Split(output, "\x00") {
		path, ok := strings.CutPrefix(field, "worktree ")
		if !ok || !sameHostState(path, state) {
			continue
		}
		_, err := matchingRuntimeFor(ctx, path, current)
		if err != nil {
			if rescueErr := preserveLocalRuntime(ctx, x, path, state); rescueErr == nil {
				_, err = matchingRuntimeFor(ctx, path, current)
			} else {
				err = fmt.Errorf("%w; local runtime preservation failed: %v", err, rescueErr)
			}
			if err != nil {
				if err := deferLinkedLauncher(entry, path, fmt.Errorf("launcher migration would lose the adopted runtime for %s: %w", path, err)); err != nil {
					return err
				}
				continue
			}
		}
		if err := InstallRuntime(path, binary, ""); err != nil {
			if err := deferLinkedLauncher(entry, path, err); err != nil {
				return err
			}
		}
	}
	return nil
}

func deferLinkedLauncher(entry Entry, path string, cause error) error {
	if sameDirectory(path, entry.Path) {
		return cause
	}
	// 補助 worktree の起動口は保持し、検証済みの登録先本体の展開を止めない。
	return Feedback(entry.Path, "runtime-migration", fmt.Sprintf("linked worktree %s の起動口を保持した: %v", path, cause))
}

func protectLegacyRuntime(binary string) error {
	// Unix は親の書き込み権限、Windows はファイルの読み取り専用属性で旧版の置換を拒否する。
	if err := os.Chmod(binary, 0500); err != nil {
		return err
	}
	return os.Chmod(filepath.Dir(binary), 0500)
}

func preserveLocalRuntime(ctx context.Context, x Executor, repo, state string) error {
	tree, err := engine.Open(repo)
	if err != nil {
		return err
	}
	locked, err := tree.AppliedStandard()
	tree.Close()
	if err != nil || locked == nil {
		return fmt.Errorf("cannot preserve an unidentified local runtime: %v", err)
	}
	id := runtimeIdentity{locked.Version, locked.Bundle}
	// 既存スロットの破損や別の版を、ローカルのコピーで上書きしない。
	legacy := filepath.Join(state, "bundles", id.Bundle, executableName())
	for _, target := range []string{id.path(state), legacy} {
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return fmt.Errorf("runtime slot is already present: %s", target)
		}
	}
	local := filepath.Join(repo, ".standards/cache", executableName())
	actual, err := inspectRuntime(ctx, x, local)
	if err != nil {
		return err
	}
	if actual != id {
		return fmt.Errorf("local runtime does not match the adoption")
	}
	// 旧セレクタも自分自身で終端できるよう、一致する旧形式スロットも保存する。
	if _, err := publishRuntimeAt(ctx, x, local, state, id, legacy); err != nil {
		return err
	}
	if err := protectLegacyRuntime(legacy); err != nil {
		return err
	}
	_, err = publishRuntime(ctx, x, local, state, id)
	return err
}

func sameHostState(repo, state string) bool {
	located := hostState(repo)
	return located != "" && sameDirectory(located, state)
}

func sameDirectory(leftPath, rightPath string) bool {
	if filepath.Clean(leftPath) == filepath.Clean(rightPath) {
		return true
	}
	left, leftErr := os.Stat(leftPath)
	right, rightErr := os.Stat(rightPath)
	return leftErr == nil && rightErr == nil && os.SameFile(left, right)
}
func readFeedback(c Config, state string) (string, error) {
	var texts []string
	entries := append([]Entry{c.Source}, c.Projects...)
	for _, e := range entries {
		paths, _ := filepath.Glob(filepath.Join(e.Path, ".standards/cache/feedback/*.json"))
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			if len(b) > 8192 {
				return "", fmt.Errorf("feedback too large")
			}
			if _, err := os.Stat(filepath.Join(state, "handled", hash(string(b)))); err == nil {
				continue
			}
			texts = append(texts, string(b))
			if len(texts) >= 20 {
				return strings.Join(texts, "\n"), nil
			}
		}
	}
	return strings.Join(texts, "\n"), nil
}
func archiveFeedback(c Config, state, text string) error {
	if text == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(state, "handled"), 0700); err != nil {
		return err
	}
	entries := append([]Entry{c.Source}, c.Projects...)
	for _, e := range entries {
		paths, _ := filepath.Glob(filepath.Join(e.Path, ".standards/cache/feedback/*.json"))
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if strings.Contains(text, string(b)) {
				if err := os.WriteFile(filepath.Join(state, "handled", hash(string(b))), []byte(time.Now().UTC().Format(time.RFC3339)), 0600); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
