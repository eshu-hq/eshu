// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

const (
	// Directory depth statements can carry many row maps on large Java repos.
	// Keep the grouped transaction under NornicDB's request-size ceiling without
	// changing directory ordering or row identity.
	defaultNornicDBDirectoryPhaseStatements = 5
)
