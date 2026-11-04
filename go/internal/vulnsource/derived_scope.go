// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

// maxDerivedOSVTargetQueries bounds the queries one derived OSV scope may name.
// It is the coordinator's per-target query chunk; a wider scope fails closed.
const maxDerivedOSVTargetQueries = 100

// derivedOSVVirtualSlotCount bounds the virtual slot ordinals of pinned slot
// scopes. It duplicates workflow.OwnedPackageVirtualSlotCount (#9432) on
// purpose: importing workflow here would pull the reducer tree into this
// dependency-light package. TestDerivedOSVVirtualSlotCountMatchesWorkflow pins
// the two together, so a change on one side fails loudly instead of silently
// planning scopes this parser reads as foreign.
const derivedOSVVirtualSlotCount = 1024

// ParseDerivedOSVSlotScope validates a pinned virtual-slot scope id of the
// form vuln-intel://osv/<ecosystem>/_slot/<vslot>/<chunk> with no query
// string, and returns its ecosystem, virtual slot, and chunk ordinal. It
// reports false for every other scope id, including legacy _batch and
// single-package scopes. Parsing is validation only: the queries behind a
// slot scope ride the claim transport, never the scope id.
func ParseDerivedOSVSlotScope(scopeID string) (ecosystem string, virtualSlot int, chunk int, ok bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != "vuln-intel" || parsed.Host != SourceOSV {
		return "", 0, 0, false
	}
	if parsed.RawQuery != "" {
		return "", 0, 0, false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[1] != "_slot" {
		return "", 0, 0, false
	}
	ecosystem, err = url.PathUnescape(parts[0])
	if err != nil {
		return "", 0, 0, false
	}
	if ecosystem = packageidentity.LowerASCII(strings.TrimSpace(ecosystem)); ecosystem == "" {
		return "", 0, 0, false
	}
	vslot, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil || vslot < 0 || vslot >= derivedOSVVirtualSlotCount {
		return "", 0, 0, false
	}
	ordinal, err := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err != nil || ordinal < 0 {
		return "", 0, 0, false
	}
	return ecosystem, vslot, ordinal, true
}

// IsDerivedOSVSlotScope reports whether scopeID has the pinned virtual-slot
// shape. It performs no ecosystem normalization; callers that need the
// ecosystem use [ParseDerivedOSVSlotScope].
func IsDerivedOSVSlotScope(scopeID string) bool {
	_, _, _, ok := ParseDerivedOSVSlotScope(scopeID)
	return ok
}

// DerivedScopePackageKeys returns the package keys a coordinator-minted
// derived OSV scope id records. The extension host's source-state recorder
// calls it for a claim whose collector already accepted the scope, and the keys
// depend only on the scope id. It reports false for a scope id that is not a
// derived OSV scope (a configured target, another source, a pinned slot scope,
// or a malformed id), and yields no keys for an ecosystem that does not
// normalize, so an unknown ecosystem fails closed.
func DerivedScopePackageKeys(scopeID string) ([]PackageKey, bool) {
	ecosystem, names, ok := parseDerivedOSVScope(scopeID)
	if !ok {
		return nil, false
	}
	return derivedTargetPackageKeys(ecosystem, names), true
}

// SlotScopePackageKeys returns the package keys for a slot claim whose queries
// the caller resolved through the claim transport (the run's
// requested_scope_set), in the same canonical form the scope-id parser yields
// for legacy scopes. It reports false when the ecosystem does not normalize or
// the name list is empty or larger than one chunk, failing closed exactly as
// the scope-id parser does.
func SlotScopePackageKeys(ecosystem string, packageNames []string) ([]PackageKey, bool) {
	if len(packageNames) == 0 || len(packageNames) > maxDerivedOSVTargetQueries {
		return nil, false
	}
	ecosystem = packageidentity.LowerASCII(strings.TrimSpace(ecosystem))
	names := make([]string, 0, len(packageNames))
	for _, name := range packageNames {
		if strings.TrimSpace(name) == "" {
			return nil, false
		}
		names = append(names, strings.TrimSpace(name))
	}
	keys := derivedTargetPackageKeys(ecosystem, names)
	if len(keys) == 0 {
		return nil, false
	}
	return keys, true
}

// parseDerivedOSVScope parses a coordinator-minted derived OSV scope id and
// returns its ecosystem and one package name per query.
//
// Pinned virtual-slot scopes (vuln-intel://osv/<eco>/_slot/<vslot>/<chunk>,
// #9432) are deliberately not accepted here: the scope id carries no queries.
// They resolve through the claim transport with [SlotScopePackageKeys];
// accepting them here would record no package keys.
func parseDerivedOSVScope(scopeID string) (ecosystem string, names []string, ok bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != "vuln-intel" || parsed.Host != SourceOSV {
		return "", nil, false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 {
		return "", nil, false
	}
	ecosystem, err = url.PathUnescape(parts[0])
	if err != nil {
		return "", nil, false
	}
	ecosystem = packageidentity.LowerASCII(strings.TrimSpace(ecosystem))
	packageName, err := url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(packageName) == "" {
		return "", nil, false
	}
	if packageName == "_batch" {
		names = cleanDerivedOSVQueryBatch(parsed.Query()["query"])
		if len(names) == 0 || len(names) > maxDerivedOSVTargetQueries {
			return "", nil, false
		}
		return ecosystem, names, true
	}
	versions := cleanDerivedOSVVersions(parsed.Query()["version"])
	if len(versions) == 0 || len(versions) > maxDerivedOSVTargetQueries {
		return "", nil, false
	}
	names = make([]string, 0, len(versions))
	for range versions {
		names = append(names, packageName)
	}
	return ecosystem, names, true
}

// derivedTargetPackageKeys returns one key per consumption-key candidate of
// every queried package, under the canonical packageidentity ecosystem (the
// derived scope says "go"; the key says "gomod"). An ecosystem that does not
// normalize yields no keys, so an unknown ecosystem fails closed and its
// scope reads as unmatched rather than guessed.
func derivedTargetPackageKeys(ecosystem string, names []string) []PackageKey {
	canonical := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))
	if canonical == "" {
		return nil
	}
	keys := packageidentity.ConsumptionKeys(string(canonical), names...)
	out := make([]PackageKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, PackageKey{Ecosystem: string(key.Ecosystem), PackageName: key.PackageName})
	}
	return out
}

// cleanDerivedOSVVersions trims the version values of a single-package scope,
// dropping blanks and repeats and keeping first-seen order.
func cleanDerivedOSVVersions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		version := strings.TrimSpace(value)
		if version == "" {
			continue
		}
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		out = append(out, version)
	}
	return out
}

// cleanDerivedOSVQueryBatch reads the name@version query values of a _batch
// scope and returns the package name of each distinct query, keeping
// first-seen order. A value with no name or no version is dropped.
func cleanDerivedOSVQueryBatch(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		raw := strings.TrimSpace(value)
		separator := strings.LastIndex(raw, "@")
		if separator <= 0 || separator == len(raw)-1 {
			continue
		}
		packageName := strings.TrimSpace(raw[:separator])
		version := strings.TrimSpace(raw[separator+1:])
		if packageName == "" || version == "" {
			continue
		}
		key := packageName + "\x00" + version
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, packageName)
	}
	return out
}
