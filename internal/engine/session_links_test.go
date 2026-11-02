package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReceiptForInternalSymlinkDoesNotFollowExternalFiles(t *testing.T) {
	tree := fixture(t)
	write(t, tree.Root, "AGENTS.md", "# Instructions\n")
	if err := os.Symlink("AGENTS.md", filepath.Join(tree.Root, "CLAUDE.md")); err != nil {
		t.Skip(err)
	}
	start, err := tree.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.WriteReceipt(start); err != nil {
		t.Fatal(err)
	}
	write(t, tree.Root, "AGENTS.md", "# Changed instructions\n")
	if tree.ReceiptValid() {
		t.Fatal("link target edit did not invalidate receipt")
	}
	os.Remove(filepath.Join(tree.Root, "CLAUDE.md"))
	outside := filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(outside, []byte("do not read"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree.Root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Fingerprint(); err == nil {
		t.Fatal("external link accepted")
	}
	// Managed files still cannot be links, even when the target is internal.
	os.Remove(filepath.Join(tree.Root, "CLAUDE.md"))
	if err := os.Symlink("AGENTS.md", filepath.Join(tree.Root, "mise.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md"}); err == nil {
		t.Fatal("managed symlink accepted")
	}
}

func TestFingerprintSubmoduleInitializationAndEdits(t *testing.T) {
	tree := fixture(t)
	commit(t, tree.Root)
	childDir := filepath.Join(tree.Root, "component")
	if err := os.Mkdir(childDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--quiet", childDir}, {"-C", tree.Root, "update-index", "--add", "--cacheinfo", "160000," + tree.head() + ",component"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	write(t, childDir, "source.txt", "first\n")
	commit(t, childDir)
	start, err := tree.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	write(t, childDir, "source.txt", "second\n")
	changed, err := tree.Fingerprint()
	if err != nil || start == changed {
		t.Fatalf("submodule change not captured: %v", err)
	}
	// Only this test-created submodule metadata is removed to model an uninitialized checkout.
	if err := os.RemoveAll(filepath.Join(childDir, ".git")); err != nil {
		t.Fatal(err)
	}
	uninitialized, err := tree.Fingerprint()
	if err != nil || uninitialized == changed {
		t.Fatalf("uninitialized checkout: %v", err)
	}
}
