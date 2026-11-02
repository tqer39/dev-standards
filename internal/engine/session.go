package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	bundle "github.com/tqer39/dev-standards"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Fingerprint includes tracked and non-ignored files, including deletions.
// Receipts are ignored cache files and cannot validate their own contents.
func (t *Tree) Fingerprint() (string, error) {
	b, err := exec.Command("git", "-C", t.Root, "ls-files", "--cached", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return "", err
	}
	paths := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	sort.Strings(paths)
	h := sha256.New()
	fmt.Fprintln(h, t.head())
	seen := map[string]bool{}
	var total int64
	for _, p := range paths {
		if p == "" || seen[p] || strings.HasPrefix(p, ".standards/cache/") || strings.HasPrefix(p, ".local/") {
			continue
		}
		seen[p] = true
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
		info, err := f.Stat()
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
