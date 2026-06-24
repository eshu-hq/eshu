// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"sort"
	"strings"
)

// AssertionClaims is the validation-safe claim material extracted from SAML.
type AssertionClaims struct {
	NameID     string
	Attributes map[string][]string
}

// ClaimMapping describes which assertion attributes carry IdP group names.
type ClaimMapping struct {
	GroupAttributeNames []string
	RequireGroups       bool
	HashScope           string
}

// Principal is the Eshu-shaped SAML subject used by storage and session logic.
type Principal struct {
	ExternalSubjectHash string
	GroupClaimHash      string
	GroupKeys           []string
}

// NormalizeClaims converts SAML claims into hash-safe subject and group data.
func NormalizeClaims(claims AssertionClaims, mapping ClaimMapping) (Principal, error) {
	nameID := strings.TrimSpace(claims.NameID)
	if nameID == "" {
		return Principal{}, ErrNameIDMissing
	}
	groupKeys := normalizeGroups(claims.Attributes, mapping.GroupAttributeNames)
	if mapping.RequireGroups && len(groupKeys) == 0 {
		return Principal{}, ErrMissingGroupClaims
	}
	scope := strings.TrimSpace(mapping.HashScope)
	hashParts := append([]string{"saml-groups", scope}, groupKeys...)
	return Principal{
		ExternalSubjectHash: stableHash("saml-subject", scope, nameID),
		GroupClaimHash:      stableHash(hashParts...),
		GroupKeys:           groupKeys,
	}, nil
}

func normalizeGroups(attributes map[string][]string, names []string) []string {
	if len(attributes) == 0 || len(names) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, value := range attributes[name] {
			normalized := strings.ToLower(strings.TrimSpace(value))
			if normalized == "" {
				continue
			}
			seen[normalized] = struct{}{}
		}
	}
	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}
