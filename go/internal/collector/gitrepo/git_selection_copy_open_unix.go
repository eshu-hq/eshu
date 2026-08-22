// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package gitrepo

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type managedCopySourceRoot struct {
	sourceRoot   string
	directoryFDs map[string]int
}

func openManagedCopySourceRoot(sourceRoot string) (*managedCopySourceRoot, error) {
	rootFD, err := unix.Open(sourceRoot, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open managed-copy root", Path: sourceRoot, Err: err}
	}
	return &managedCopySourceRoot{
		sourceRoot: sourceRoot,
		directoryFDs: map[string]int{
			".": rootFD,
		},
	}, nil
}

func (r *managedCopySourceRoot) PinDirectory(relativePath string) error {
	relativePath = filepath.Clean(filepath.FromSlash(relativePath))
	if relativePath == "." || filepath.IsAbs(relativePath) {
		return nil
	}
	if _, exists := r.directoryFDs[relativePath]; exists {
		return nil
	}
	parentPath := filepath.Dir(relativePath)
	parentFD, exists := r.directoryFDs[parentPath]
	if !exists {
		return &os.PathError{Op: "pin managed-copy directory", Path: relativePath, Err: os.ErrNotExist}
	}
	directoryFD, err := unix.Openat(
		parentFD,
		filepath.Base(relativePath),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return &os.PathError{Op: "pin managed-copy directory", Path: relativePath, Err: err}
	}
	r.directoryFDs[relativePath] = directoryFD
	return nil
}

func (r *managedCopySourceRoot) TrimToParent(relativePath string) error {
	parentPath := filepath.Dir(filepath.Clean(filepath.FromSlash(relativePath)))
	for pinnedPath, directoryFD := range r.directoryFDs {
		if pinnedPath == "." || pinnedPath == parentPath ||
			strings.HasPrefix(parentPath, pinnedPath+string(filepath.Separator)) {
			continue
		}
		if err := unix.Close(directoryFD); err != nil {
			return &os.PathError{Op: "close managed-copy directory", Path: pinnedPath, Err: err}
		}
		delete(r.directoryFDs, pinnedPath)
	}
	return nil
}

func (r *managedCopySourceRoot) OpenFile(relativePath string) (*os.File, error) {
	relativePath = filepath.Clean(filepath.FromSlash(relativePath))
	if relativePath == "." || filepath.IsAbs(relativePath) {
		return nil, &os.PathError{Op: "open managed-copy file", Path: relativePath, Err: os.ErrInvalid}
	}
	parentFD, exists := r.directoryFDs[filepath.Dir(relativePath)]
	if !exists {
		return nil, &os.PathError{Op: "open managed-copy file", Path: relativePath, Err: os.ErrNotExist}
	}
	fileFD, err := unix.Openat(
		parentFD,
		filepath.Base(relativePath),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open managed-copy file", Path: relativePath, Err: err}
	}
	file := os.NewFile(uintptr(fileFD), filepath.Join(r.sourceRoot, relativePath))
	if file == nil {
		_ = unix.Close(fileFD)
		return nil, &os.PathError{Op: "open managed-copy file", Path: relativePath, Err: os.ErrInvalid}
	}
	return file, nil
}

func (r *managedCopySourceRoot) Close() error {
	var firstErr error
	for relativePath, directoryFD := range r.directoryFDs {
		if err := unix.Close(directoryFD); err != nil && firstErr == nil {
			firstErr = &os.PathError{Op: "close managed-copy directory", Path: relativePath, Err: err}
		}
	}
	r.directoryFDs = nil
	return firstErr
}

func (r *managedCopySourceRoot) pinnedDirectoryCount() int {
	return len(r.directoryFDs)
}
