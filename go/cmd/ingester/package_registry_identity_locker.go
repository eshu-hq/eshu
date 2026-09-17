// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func packageRegistryIdentityLocker(database db.ExecQueryer) projector.PackageRegistryIdentityLocker {
	if beginner, ok := database.(db.Beginner); ok {
		return postgres.PackageRegistryIdentityLocker{DB: beginner}
	}
	return nil
}
