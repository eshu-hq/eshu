// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileSystem abstracts the file operations the guidance flows need so tests can
// inject a fault-injecting implementation and exercise the write and delete
// error paths without a real failing disk.
//
// Every method takes an absolute path Engine composed from its own root; this
// interface is not a general filesystem seam and must not grow one.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
}

// OSFileSystem is the production FileSystem backed by the os package.
//
// Each method returns the os error unwrapped on purpose. readFileOrEmpty adds
// the path context (`read <path>: ...`), so wrapping here would print the path
// twice, and Engine's not-exist check goes through os.IsNotExist, which does
// NOT unwrap a %w-wrapped error -- a wrap here would silently turn "file does
// not exist" into a hard failure.
type OSFileSystem struct{}

// ReadFile reads the named file.
//
//nolint:wrapcheck // see OSFileSystem: os.IsNotExist does not unwrap, and the caller adds path context.
func (OSFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec G304 -- path is a project-directory file path composed by Engine from its own root, not an HTTP request param
}

// WriteFile writes data to the named file, creating it with perm when absent.
//
//nolint:wrapcheck // see OSFileSystem: the caller adds path context.
func (OSFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// MkdirAll creates the named directory and any missing parents.
//
//nolint:wrapcheck // see OSFileSystem: the caller adds path context.
func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

// Remove deletes the named file.
//
//nolint:wrapcheck // see OSFileSystem: the caller adds path context.
func (OSFileSystem) Remove(path string) error { return os.Remove(path) }

// Engine performs install/status/uninstall against an injectable FileSystem
// rooted at a project directory. The root must already be absolute; resolving
// it from a flag or the process working directory is the caller's job.
type Engine struct {
	fs   FileSystem
	root string
}

// NewEngine builds an Engine for an absolute project root, backed by the
// production filesystem.
func NewEngine(root string) *Engine {
	return &Engine{fs: OSFileSystem{}, root: root}
}

// NewEngineWithFS builds an Engine over a caller-supplied FileSystem. Tests use
// it to drive the write and delete failure paths.
func NewEngineWithFS(fs FileSystem, root string) *Engine {
	return &Engine{fs: fs, root: root}
}

// Root returns the absolute project root this Engine operates on.
func (e *Engine) Root() string { return e.root }

// Result records the outcome of an install/status/uninstall action for one
// platform, used to render output and assert in tests.
type Result struct {
	// Platform is the assistant this result describes.
	Platform Platform
	// Path is the absolute instruction-file path the action targeted.
	Path string
	// Status is the managed-block state after the action.
	Status BlockStatus
	// Changed reports whether the file content was modified by the action.
	Changed bool
	// Created reports whether the action created a new file.
	Created bool
	// Removed reports whether uninstall deleted a now-empty Eshu-created file.
	Removed bool
}

// SelectPlatforms returns the platforms to operate on, honoring the --platform
// filter value the caller read from its flag. An unknown filter is an error so
// unsupported platforms are explicit rather than silently skipped.
func SelectPlatforms(filter string) ([]Platform, error) {
	if filter == "" {
		return SupportedPlatforms(), nil
	}
	p, ok := LookupPlatform(strings.ToLower(strings.TrimSpace(filter)))
	if !ok {
		return nil, fmt.Errorf("unsupported assistant platform %q (supported: claude, codex, cursor)", filter)
	}
	return []Platform{p}, nil
}

// readFileOrEmpty returns the file content, or empty string when the file does
// not exist. Any other read error is returned with the path attached.
func (e *Engine) readFileOrEmpty(path string) (string, bool, error) {
	data, err := e.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), true, nil
}

// Install writes or refreshes the managed block for each selected platform,
// preserving any pre-existing file content outside the managed block. A file
// whose content is already byte-identical is not rewritten at all.
func (e *Engine) Install(platforms []Platform) ([]Result, error) {
	results := make([]Result, 0, len(platforms))
	for _, p := range platforms {
		path := filepath.Join(e.root, p.RelPath)
		existing, existed, err := e.readFileOrEmpty(path)
		if err != nil {
			return nil, err
		}
		body := GuidanceBody(p)
		updated := Upsert(existing, body)
		res := Result{Platform: p, Path: path, Status: Classify(updated, body)}
		if updated == existing {
			results = append(results, res)
			continue
		}
		// Ensure the parent directory exists (Cursor rules live under
		// .cursor/rules/). MkdirAll is a no-op when the directory already exists.
		if err := e.fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", path, err)
		}
		if err := e.fs.WriteFile(path, []byte(updated), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		res.Changed = true
		res.Created = !existed
		results = append(results, res)
	}
	return results, nil
}

// Status reports the managed-block state for each selected platform without
// modifying any file.
func (e *Engine) Status(platforms []Platform) ([]Result, error) {
	results := make([]Result, 0, len(platforms))
	for _, p := range platforms {
		path := filepath.Join(e.root, p.RelPath)
		existing, _, err := e.readFileOrEmpty(path)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{
			Platform: p,
			Path:     path,
			Status:   Classify(existing, GuidanceBody(p)),
		})
	}
	return results, nil
}

// Uninstall removes the managed block for each selected platform. It deletes a
// file only when that file becomes empty AND Eshu created it (the file is
// nothing but the managed block). Files with other content are preserved with
// just the block stripped; files Eshu did not create are never deleted.
func (e *Engine) Uninstall(platforms []Platform) ([]Result, error) {
	results := make([]Result, 0, len(platforms))
	for _, p := range platforms {
		path := filepath.Join(e.root, p.RelPath)
		existing, existed, err := e.readFileOrEmpty(path)
		if err != nil {
			return nil, err
		}
		res := Result{Platform: p, Path: path, Status: BlockAbsent}
		if !existed {
			results = append(results, res)
			continue
		}
		updated, removed := Remove(existing)
		if !removed {
			results = append(results, res)
			continue
		}
		res.Changed = true
		// Delete only a file that is now empty: that means it held nothing but
		// the Eshu block, so Eshu effectively owned it. Never delete a file that
		// still has user content.
		if strings.TrimSpace(updated) == "" {
			if err := e.fs.Remove(path); err != nil {
				return nil, fmt.Errorf("remove %s: %w", path, err)
			}
			res.Removed = true
			results = append(results, res)
			continue
		}
		if err := e.fs.WriteFile(path, []byte(updated), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		results = append(results, res)
	}
	return results, nil
}
