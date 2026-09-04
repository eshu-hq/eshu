// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// This file holds the payload/delta forwarders that used to live in the
// semantic_entity_*.go files before the semantic_entity family moved to
// [semanticentity] (issue #6061). Each one already forwarded to a
// shared-tier package; they stay in root because other root families that
// have not moved out yet still call them by their unqualified root spelling.
// semanticentity calls the shared-tier functions directly instead of
// reaching back into root for these.

// payloadMap forwards to [payloadcore.PayloadMap].
func payloadMap(payload map[string]any, key string) map[string]any {
	return payloadcore.PayloadMap(payload, key)
}

// semanticPayloadString forwards to [payloadcore.SemanticPayloadString].
func semanticPayloadString(payload map[string]any, key string) string {
	return payloadcore.SemanticPayloadString(payload, key)
}

// semanticPayloadStringSlice forwards to [payloadcore.SemanticPayloadStringSlice].
func semanticPayloadStringSlice(payload map[string]any, key string) []string {
	return payloadcore.SemanticPayloadStringSlice(payload, key)
}

// semanticQualifyDeltaPath forwards to [payloadcore.QualifyDeltaPath].
func semanticQualifyDeltaPath(repoPath string, relativePath string) string {
	return payloadcore.QualifyDeltaPath(repoPath, relativePath)
}

// semanticDeltaPayloadBool forwards to [payloadcore.DeltaPayloadBool].
func semanticDeltaPayloadBool(payload map[string]any, key string) bool {
	return payloadcore.DeltaPayloadBool(payload, key)
}

// deltaScopeRepositorySet forwards to [sharedintent.DeltaScopeRepositorySet].
func deltaScopeRepositorySet(repositoryIDs []string) map[string]struct{} {
	return sharedintent.DeltaScopeRepositorySet(repositoryIDs)
}

// applyRepoRefreshDeltaScope forwards to
// [sharedintent.ApplyRepoRefreshDeltaScope], which carries the full rule and
// why the two obvious alternatives lose edges (#6216).
func applyRepoRefreshDeltaScope(
	payload map[string]any,
	repoID string,
	deltaRepositoryIDs map[string]struct{},
	filePathsByRepoID map[string][]string,
) {
	sharedintent.ApplyRepoRefreshDeltaScope(payload, repoID, deltaRepositoryIDs, filePathsByRepoID)
}
