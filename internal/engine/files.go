package engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

const maxFile = 1 << 20

type Tree struct {
	Root string
	fs   *os.Root
}

func Open(root string) (*Tree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	t := &Tree{abs, f}
	cmd := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel")
	b, err := cmd.Output()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("target must be a Git repository root")
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(b)))
	if err != nil || top != abs {
		f.Close()
		return nil, fmt.Errorf("target must be the Git repository root")
	}
	return t, nil
}
func (t *Tree) Close() error { return t.fs.Close() }
func validPath(p string) error {
	if p == "" || p == "." || strings.ContainsAny(p, "\\:\x00\r\n") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return fmt.Errorf("invalid relative path: %q", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || strings.EqualFold(part, ".git") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return fmt.Errorf("unsafe path: %q", p)
		}
	}
	return nil
}
func (t *Tree) safe(p string) error {
	if err := validPath(p); err != nil {
		return err
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		info, err := t.fs.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", p)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", p)
		}
	}
	return nil
}
func (t *Tree) Read(p string) (Snapshot, error) {
	if err := t.safe(p); err != nil {
		return Snapshot{}, err
	}
	info, err := t.fs.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("regular file required: %s", p)
	}
	if info.Size() > maxFile {
		return Snapshot{}, fmt.Errorf("file exceeds 1 MiB: %s", p)
	}
	f, err := t.fs.Open(p)
	if err != nil {
		return Snapshot{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return Snapshot{}, err
	}
	if len(b) > maxFile || !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return Snapshot{}, fmt.Errorf("bounded UTF-8 text required: %s", p)
	}
	mode := uint32(info.Mode().Perm())
	if runtime.GOOS == "windows" {
		mode = 0644
	}
	return Snapshot{true, string(b), mode}, nil
}
func (t *Tree) head() string {
	b, err := exec.Command("git", "-C", t.Root, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		return "unborn"
	}
	return strings.TrimSpace(string(b))
}
func (t *Tree) write(p string, s Snapshot) error {
	if err := t.safe(p); err != nil {
		return err
	}
	if !s.Exists {
		return t.fs.Remove(p)
	}
	if err := t.fs.MkdirAll(path.Dir(p), 0755); err != nil {
		return err
	}
	if err := t.safe(p); err != nil {
		return err
	}
	// os.Root confines both the staging file and the destination to the tree.
	name := p + ".dev-standards-tmp"
	if err := t.safe(name); err != nil {
		return err
	}
	f, err := t.fs.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(s.Mode))
	if err != nil {
		return err
	}
	defer t.fs.Remove(name)
	if _, err = f.WriteString(s.Content); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(os.FileMode(s.Mode)); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return t.fs.Rename(name, p)
}
