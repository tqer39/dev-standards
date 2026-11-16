package automation

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutableCopyPreservesIdenticalDestination(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, executableName())
	if err := os.WriteFile(source, []byte("検証済みの実行ファイル"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1000000000, 0)
	if err := os.Chtimes(destination, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err != nil {
		t.Fatalf("同一内容の再適用: %v", err)
	}
	after, err := os.Stat(destination)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("同一内容の起動口を置換しました: %v", err)
	}
}

func TestExecutableCopyUpdatesSameSizeChangedContents(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, executableName())
	if err := os.WriteFile(source, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "new" {
		t.Fatalf("同じサイズの変更を取りこぼしました: %s %v", data, err)
	}
}

func TestExecutableCopyRestoresRequiredMode(t *testing.T) {
	if runtimeWindows() {
		t.Skip("Windows は Unix の実行権限ビットを使用しない")
	}
	for _, mode := range []os.FileMode{0600, 0700 | os.ModeSetuid, 0700 | os.ModeSetgid} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "source"), filepath.Join(dir, executableName())
			for _, name := range []string{source, destination} {
				if err := os.WriteFile(name, []byte("same"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(destination, mode); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode() != mode {
				t.Skipf("要求したモードを設定できません: %v", before.Mode())
			}
			if err := copyExecutable(source, destination); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(destination)
			if err != nil || info.Mode() != 0700 {
				t.Fatalf("起動口の権限を復元しませんでした: %v", err)
			}
		})
	}
}

func TestExecutableCopyPreservesFilesOnWriteConflict(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, executableName())
	for name, content := range map[string]string{source: "new", destination: "old", destination + ".new": "書き込み中の別の内容"} {
		if err := os.WriteFile(name, []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyExecutable(source, destination); err == nil {
		t.Fatal("書き込み衝突を成功として扱いました")
	}
	for name, want := range map[string]string{destination: "old", destination + ".new": "書き込み中の別の内容"} {
		if data, err := os.ReadFile(name); err != nil || string(data) != want {
			t.Fatalf("衝突時に既存ファイルを変更しました: %s %v", name, err)
		}
	}
}

func TestExecutableCopyRejectsIdenticalSymlink(t *testing.T) {
	dir := t.TempDir()
	source, target, destination := filepath.Join(dir, "source"), filepath.Join(dir, "target"), filepath.Join(dir, executableName())
	for _, name := range []string{source, target} {
		if err := os.WriteFile(name, []byte("same"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, destination); err != nil {
		t.Skipf("シンボリックリンクを作成できません: %v", err)
	}
	if err := copyExecutable(source, destination); err == nil {
		t.Fatal("同一内容のシンボリックリンクを受け入れました")
	}
	if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("既存のリンクを変更しました: %v", err)
	}
}
