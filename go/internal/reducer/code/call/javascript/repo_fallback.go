// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// BlocksRepoFallback reports whether a JavaScript-family call bound to a bare
// package import must not fall back to a same-named in-repository declaration
// (#7610). The parser keys such a call with package_export_symbol =
// "package:<name>#<export>" only when a package.json declares the imported
// name, so an unresolved key names an explicitly imported but unresolvable
// target: falling back to UniqueNameByRepo would invent a wrong edge to the
// consumer's own same-named function. The fallback stays only when the key's
// package is one of the consumer repository's own manifest names (a
// same-repository workspace package whose export is not keyed yet, such as an
// export list or a CommonJS module), where the fallback is the true edge. A
// call with no key, or with a key in an unexpected shape, never blocks: the
// barrier fails open to the previous behavior.
func BlocksRepoFallback(ctx shared.ResolveContext) bool {
	packageName := nodePackageKeyPackage(payloadcore.AnyToString(ctx.Call["package_export_symbol"]))
	if packageName == "" {
		return false
	}
	return !ctx.Index.RepoPublishesNodePackage(ctx.RepositoryID, packageName)
}

// nodePackageKeyPackage extracts the package name from a
// "package:<name>#<export>" symbol key, or "" when the key has an unexpected
// shape. It cuts at the last "#" because the export is a JavaScript
// identifier and never contains one, while the package side is kept verbatim.
func nodePackageKeyPackage(key string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(key), "package:")
	if !ok {
		return ""
	}
	hash := strings.LastIndex(rest, "#")
	if hash <= 0 || hash == len(rest)-1 {
		return ""
	}
	return rest[:hash]
}
