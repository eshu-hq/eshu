// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
)

// TypeScriptSemanticProfile aliases entitysemantics.TypeScriptSemanticProfile.
// The implementation moved to querycontract for #6060 and on to
// entitysemantics for #6060 lane A L3; this alias keeps root callers,
// including the Present and Fields methods it carries, unchanged.
type TypeScriptSemanticProfile = entitysemantics.TypeScriptSemanticProfile

// TypeScriptSemanticProfileFromMetadata forwards to
// entitysemantics.TypeScriptSemanticProfileFromMetadata. The implementation
// moved to querycontract for #6060 and on to entitysemantics for #6060 lane A
// L3; this wrapper keeps root callers unchanged.
func TypeScriptSemanticProfileFromMetadata(metadata map[string]any) TypeScriptSemanticProfile {
	return entitysemantics.TypeScriptSemanticProfileFromMetadata(metadata)
}

// AttachTypeScriptSemantics forwards to
// entitysemantics.AttachTypeScriptSemantics. The implementation moved to
// querycontract for #6060 and on to entitysemantics for #6060 lane A L3;
// this wrapper keeps root callers unchanged.
func AttachTypeScriptSemantics(result map[string]any, metadata map[string]any) map[string]any {
	return entitysemantics.AttachTypeScriptSemantics(result, metadata)
}
