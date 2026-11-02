package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	bundle "github.com/tqer39/dev-standards"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Fingerprint includes tracked and non-ignored files, including deletions.
// Receipts are ignored cache files and cannot validate their own contents.
func (t *Tree) Fingerprint() (string, error) { return t.fingerprint(0) }

func (t *Tree) fingerprint(depth int) (string, error) {
	if depth > 8 {
		return "", fmt.Errorf("submodule depth exceeds fingerprint budget")
	}
	index, err := exec.Command("git", "-C", t.Root, "ls-files", "--stage", "-z").Output()
	if err != nil {
		return "", err
	}
	submodules := map[string]string{}
	for _, record := range strings.Split(string(index), "\x00") {
		metadata, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if ok && len(fields) == 3 && fields[0] == "160000" {
			if fields[2] != "0" {
				return "", fmt.Errorf("unmerged submodule: %s", name)
			}
			submodules[name] = fields[1]
		}
	}
	b, err := exec.Command("git", "-C", t.Root, "ls-files", "--cached", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return "", err
	}
	paths := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	sort.Strings(paths)
	listed := map[string]bool{}
	for _, name := range paths {
		listed[name] = true
	}
	h := sha256.New()
	fmt.Fprintln(h, t.head())
	seen := map[string]bool{}
	var total int64
	for _, p := range paths {
		if p == "" || seen[p] || strings.HasPrefix(p, ".standards/cache/") || strings.HasPrefix(p, ".local/") {
			continue
		}
		seen[p] = true
		if err := validPath(p); err != nil {
			return "", err
		}
		if parent := path.Dir(p); parent != "." {
			if err := t.safe(parent); err != nil {
				return "", err
			}
		}
		info, statErr := t.fs.Lstat(p)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			target, err := t.fs.Readlink(p)
			if err != nil {
				return "", err
			}
			resolved := path.Clean(path.Join(path.Dir(p), target))
			if path.IsAbs(target) || !listed[resolved] {
				return "", fmt.Errorf("symlink target must be a listed repository file: %s", p)
			}
			if err := t.safe(resolved); err != nil {
				return "", err
			}
			targetInfo, err := t.fs.Lstat(resolved)
			if err != nil || !targetInfo.Mode().IsRegular() {
				return "", fmt.Errorf("symlink target must be a regular file: %s", p)
			}
			fmt.Fprintf(h, "%s\x00symlink\x00%s\x00", p, target)
			continue
		}
		if sha, ok := submodules[p]; ok {
			if err := t.safe(p); err != nil {
				return "", err
			}
			fmt.Fprintf(h, "%s\x00gitlink\x00%s\x00", p, sha)
			if _, err := t.fs.Lstat(p + "/.git"); os.IsNotExist(err) {
				fmt.Fprint(h, "uninitialized\x00")
				continue
			} else if err != nil {
				return "", err
			}
			child, err := Open(filepath.Join(t.Root, p))
			if err != nil {
				return "", err
			}
			fingerprint, err := child.fingerprint(depth + 1)
			child.Close()
			if err != nil {
				return "", err
			}
			fmt.Fprint(h, fingerprint+"\x00")
			continue
		}
		if err := t.safe(p); err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00", p)
		f, err := t.fs.Open(p)
		if os.IsNotExist(err) {
			fmt.Fprint(h, "missing\x00")
			continue
		}
		if err != nil {
			return "", err
		}
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			return "", fmt.Errorf("cannot fingerprint %s", p)
		}
		if info.Size() > 16<<20 {
			f.Close()
			return "", fmt.Errorf("fingerprint file too large: %s", p)
		}
		total += info.Size()
		if total > 64<<20 {
			f.Close()
			return "", fmt.Errorf("fingerprint budget exceeded")
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
		fmt.Fprint(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type Receipt struct {
	Fingerprint string    `json:"fingerprint"`
	VerifiedAt  time.Time `json:"verifiedAt"`
	Bundle      string    `json:"bundle"`
}

func (t *Tree) WriteReceipt(start string) error {
	end, err := t.Fingerprint()
	if err != nil {
		return err
	}
	if start != end {
		return fmt.Errorf("files changed during verification")
	}
	return t.write(".standards/cache/verified.json", file(string(JSON(Receipt{end, time.Now().UTC(), bundleDigest()}))))
}
func bundleDigest() string { return bundle.Digest() }
func (t *Tree) ReceiptValid() bool {
	s, err := t.Read(".standards/cache/verified.json")
	if err != nil || !s.Exists {
		return false
	}
	var r Receipt
	if strictJSON(s.Content, &r) != nil || r.Bundle != bundleDigest() || time.Since(r.VerifiedAt) > 24*time.Hour || r.VerifiedAt.After(time.Now().Add(time.Minute)) {
		return false
	}
	fingerprint, err := t.Fingerprint()
	return err == nil && fingerprint == r.Fingerprint
}
