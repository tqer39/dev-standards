package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func strictJSON(s string, out any) error {
	if len(s) > 16<<20 {
		return fmt.Errorf("JSON exceeds 16 MiB")
	}
	dec := json.NewDecoder(bytes.NewBufferString(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("one JSON value is required")
	}
	return nil
}
func ReadPlan(path string) (*Plan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := strictJSON(string(b), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
func (t *Tree) Apply(p *Plan) error { return t.apply(p, nil) }
func (t *Tree) apply(p *Plan, beforeWrite func(int) error) error {
	if p.Root != t.Root || p.SchemaVersion != 1 {
		return fmt.Errorf("plan belongs to another root or format")
	}
	// A lock prevents concurrent applications without changing user files.
	lock, err := t.fs.OpenFile(".dev-standards-apply.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot acquire apply lock: %w", err)
	}
	lock.Close()
	defer t.fs.Remove(".dev-standards-apply.lock")
	if p.HEAD != t.head() {
		return fmt.Errorf("HEAD changed since planning; create a fresh plan")
	}
	for name, want := range p.Inputs {
		got, err := t.Read(name)
		if err != nil {
			return err
		}
		if !equal(got, want) {
			return fmt.Errorf("input changed since planning: %s", name)
		}
	}
	expected, err := t.Plan(p.Request)
	if err != nil {
		return err
	}
	if !samePlan(p, expected) {
		return fmt.Errorf("plan does not match deterministic generation; regenerate it")
	}
	if len(p.Conflicts) > 0 {
		return fmt.Errorf("resolve plan conflicts before applying: %v", p.Conflicts)
	}
	// Every output is derived again before mutation; arbitrary edited plan paths
	// or contents are never trusted, even when the caller recalculates a hash.
	missingDirs := map[string]bool{}
	for _, c := range p.Changes {
		for dir := path.Dir(c.Path); dir != "."; dir = path.Dir(dir) {
			if _, err := t.fs.Lstat(dir); os.IsNotExist(err) {
				missingDirs[dir] = true
			}
		}
	}
	var applied []Change
	rollback := func(cause error) error {
		errs := []error{cause}
		for i := len(applied) - 1; i >= 0; i-- {
			c := applied[i]
			current, e := t.Read(c.Path)
			if e != nil || !equal(current, c.After) {
				errs = append(errs, fmt.Errorf("rollback conflict: %s", c.Path))
				continue
			}
			if e := t.write(c.Path, c.Before); e != nil {
				errs = append(errs, fmt.Errorf("rollback %s: %w", c.Path, e))
			}
		}
		dirs := make([]string, 0, len(missingDirs))
		for dir := range missingDirs {
			dirs = append(dirs, dir)
		}
		sort.Slice(dirs, func(i, j int) bool { return strings.Count(dirs[i], "/") > strings.Count(dirs[j], "/") })
		for _, dir := range dirs {
			_ = t.fs.Remove(dir)
		}
		return errors.Join(errs...)
	}
	for i, c := range p.Changes {
		if beforeWrite != nil {
			if err := beforeWrite(i); err != nil {
				return rollback(err)
			}
		}
		current, err := t.Read(c.Path)
		if err != nil {
			return rollback(err)
		}
		if !equal(current, c.Before) {
			return rollback(fmt.Errorf("target changed during apply: %s", c.Path))
		}
		if err := t.write(c.Path, c.After); err != nil {
			return rollback(err)
		}
		applied = append(applied, c)
	}
	return nil
}

func SavePlan(name string, p *Plan) error {
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(JSON(p))
	closeErr := f.Close()
	return errors.Join(writeErr, closeErr)
}
