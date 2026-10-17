// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// DeriveHooks are the test seams of a Deriver, exported to the live tests of
// package artifacts_test. AfterVersionRead runs between steps 1 and 2,
// AfterLock inside step 3 right after the repository lock is granted, and
// Load replaces the per-repository function of step 2.
type DeriveHooks struct {
	AfterVersionRead func(context.Context)
	AfterLock        func(context.Context)
	Load             func(
		ctx context.Context,
		reader contentartifacts.Reader,
		repo contentartifacts.Repository,
		files []contentartifacts.File,
		hasFileList bool,
		allowance int,
	) ([]map[string]any, contentartifacts.ReadStats, error)
}

// SetDeriveHooks installs hooks on d. A nil hook keeps the production step.
func SetDeriveHooks(d *Deriver, hooks DeriveHooks) {
	d.afterVersionRead = hooks.AfterVersionRead
	d.afterLock = hooks.AfterLock
	if hooks.Load != nil {
		d.load = hooks.Load
	}
}

// NewContentReader returns the content_files reader the derive uses in step 2.
func NewContentReader(queryer db.Queryer) contentartifacts.Reader {
	return contentReader{queryer: queryer}
}

// RepoLockKey is the advisory-lock key text of a repository, for the
// lock-contention proofs.
func RepoLockKey(repoID string) string { return repoLockKey(repoID) }
