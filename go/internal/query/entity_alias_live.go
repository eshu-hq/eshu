// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_global_name_comparison

package query //nolint:dirgate // B5 tag-gated forwarder for #6060: the staying root live comparison test is the only caller, so this shim must live in package query (not entity/) and carry the test's build tag (not default tags).

import (
	"github.com/eshu-hq/eshu/go/internal/query/entity"
)

// resolveGraphEntityType maps a user-facing entity type to its graph label.
// Its home is entity/; this forwarder keeps the staying root live
// cross-store comparison test calling the package-local name. See #6060.
//
// This file carries the live_global_name_comparison build tag because the
// comparison test is the only caller: under default tags the forwarder
// would be an unused symbol. Test files do not count toward the dirgate
// file cap, but this shim is a non-test file, so the internal/query pin
// counts it like any other root file.
func resolveGraphEntityType(typeName string) (string, string, string, bool) {
	return entity.ResolveGraphEntityType(typeName)
}
