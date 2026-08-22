// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build windows

package gitrepo

import (
	"os"
	"path/filepath"
)

type managedCopySourceRoot struct {
	root *os.Root
}

func openManagedCopySourceRoot(sourceRoot string) (*managedCopySourceRoot, error) {
	root, err := os.OpenRoot(sourceRoot)
	if err != nil {
		return nil, err
	}
	return &managedCopySourceRoot{root: root}, nil
}

func (r *managedCopySourceRoot) PinDirectory(_ string) error {
	return nil
}

func (r *managedCopySourceRoot) TrimToParent(_ string) error {
	return nil
}

func (r *managedCopySourceRoot) OpenFile(relativePath string) (*os.File, error) {
	return r.root.Open(filepath.FromSlash(relativePath))
}

func (r *managedCopySourceRoot) Close() error {
	return r.root.Close()
}

func (r *managedCopySourceRoot) pinnedDirectoryCount() int {
	return 1
}
