// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

// Visibility describes source-reported package visibility when a registry
// exposes it. It travels with package identity because derived targets and
// observations record what the source claimed, not what Eshu verified.
type Visibility string

const (
	// VisibilityUnknown marks registries that did not report visibility.
	VisibilityUnknown Visibility = "unknown"
	// VisibilityPublic marks a package/feed visible without private credentials.
	VisibilityPublic Visibility = "public"
	// VisibilityPrivate marks a package/feed that requires private access.
	VisibilityPrivate Visibility = "private"
)
