// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// azureStateIdentityPrefix roots every Azure ARM resource id. The shared
// cloud_resource_uid keyspace treats Azure ids as case-insensitive (it lower-cases
// them before hashing), so the state-to-observed join must fold only identities
// of this shape and never AWS ARNs or GCP full resource names.
const azureStateIdentityPrefix = "/subscriptions/"

// azureStateFoldKey returns the case-folded match key for an Azure ARM resource
// id and reports whether the identity is Azure-shaped. Azure ARM ids are
// case-insensitive per Azure, so the key is the lower-cased identity. AWS ARNs and
// GCP full resource names are case-significant and are never folded, so a
// non-Azure identity reports false and keeps its exact-match semantics.
func azureStateFoldKey(identity string) (string, bool) {
	lowered := strings.ToLower(strings.TrimSpace(identity))
	if !strings.HasPrefix(lowered, azureStateIdentityPrefix) {
		return "", false
	}
	return lowered, true
}

// uidForMatchedStateIdentity resolves a Terraform-state native identity to the
// observed cloud_resource_uid it joins. It tries an exact, case-significant match
// first so AWS ARNs and GCP full resource names that differ only in casing never
// collapse onto a distinct observed identity, then falls back to an Azure-only
// case-folded match so an ARM id that differs only in casing from the observed
// arm_resource_id still joins. A miss returns false and the row is dropped rather
// than guessed onto the wrong uid.
func uidForMatchedStateIdentity(uidByIdentity, uidByAzureFold map[string]string, matched string) (string, bool) {
	matched = strings.TrimSpace(matched)
	if uid, ok := uidByIdentity[matched]; ok {
		return uid, true
	}
	if key, ok := azureStateFoldKey(matched); ok {
		if uid, ok := uidByAzureFold[key]; ok {
			return uid, true
		}
	}
	return "", false
}
