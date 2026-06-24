// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"sort"
	"strings"
)

// PackageRegistryIdentityLocker coordinates package identity graph writes
// across projector runtimes that may run in separate processes.
type PackageRegistryIdentityLocker interface {
	WithPackageRegistryIdentityLocks(context.Context, []string, func(context.Context) error) error
}

func (r Runtime) withPackageRegistryIdentityLocks(
	ctx context.Context,
	mat CanonicalMaterialization,
	fn func(context.Context) error,
) error {
	if fn == nil {
		return nil
	}
	packageIDs := packageRegistryIdentityLockIDs(mat)
	if r.PackageRegistryIdentityLocker == nil || len(packageIDs) == 0 {
		return fn(ctx)
	}
	return r.PackageRegistryIdentityLocker.WithPackageRegistryIdentityLocks(ctx, packageIDs, fn)
}

func packageRegistryIdentityLockIDs(mat CanonicalMaterialization) []string {
	keys := make(
		[]string,
		0,
		len(mat.PackageRegistryPackages)+len(mat.PackageRegistryVersions)+2*len(mat.PackageRegistryDependencies),
	)
	for _, row := range mat.PackageRegistryPackages {
		keys = append(keys, row.UID)
	}
	for _, row := range mat.PackageRegistryVersions {
		keys = append(keys, row.PackageID)
	}
	for _, row := range mat.PackageRegistryDependencies {
		keys = append(keys, row.PackageID, row.DependencyPackageID)
	}
	return uniqueSortedPackageRegistryIdentityLockIDs(keys)
}

func uniqueSortedPackageRegistryIdentityLockIDs(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
