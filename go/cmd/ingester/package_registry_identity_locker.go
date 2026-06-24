// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func packageRegistryIdentityLocker(database postgres.ExecQueryer) projector.PackageRegistryIdentityLocker {
	if beginner, ok := database.(postgres.Beginner); ok {
		return postgres.PackageRegistryIdentityLocker{DB: beginner}
	}
	return nil
}
