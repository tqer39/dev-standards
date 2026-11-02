package automation

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

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
