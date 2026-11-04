// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"strings"
	"time"
)

// Source tokens stored in vulnerability_source_states.source. They are the
// values the supply-chain readiness reader matches against (see
// target_vulnerability_source_scopes in the readiness query), so a writer
// that stores any other token makes its rows invisible to readiness.
const (
	// SourceOSV names OSV advisory targets (vuln-intel://osv/...).
	SourceOSV = "osv"
	// SourceNVD names NVD CVE targets (vuln-intel://nvd/...).
	SourceNVD = "nvd"
	// SourceCISAKEV names the CISA Known Exploited Vulnerabilities catalog
	// target (vuln-intel://cisa/kev).
	SourceCISAKEV = "cisa_kev"
	// SourceFirstEPSS names the FIRST EPSS score target
	// (vuln-intel://first/epss, or the coordinator-derived
	// vuln-intel://first_epss/_findings/<chunk>).
	SourceFirstEPSS = "first_epss"
	// SourceGLAD names GitLab Advisory Database targets (vuln-intel://glad/...).
	SourceGLAD = "glad"
	// SourceGHSA names GitHub Advisory Database targets
	// (vuln-intel://ghsa/reviewed and vuln-intel://ghsa/malware). Unreviewed
	// GitHub advisories are out of scope and have no scope id.
	SourceGHSA = "ghsa"
)

// DefaultFreshnessWindow is how long a successful source observation stays
// fresh before a writer records it as stale. The in-tree collector and the
// component-extension host recorder share this one window.
const DefaultFreshnessWindow = 14 * 24 * time.Hour

const scopeIDPrefix = "vuln-intel://"

// SourceForScopeID derives the source token from a vulnerability scope id.
// The scope-id grammar is core-owned: the workflow coordinator mints
// vuln-intel://osv/<ecosystem>/... and vuln-intel://first_epss/_findings/<chunk>
// for derived targets, and configured targets use vuln-intel://nvd/...,
// vuln-intel://cisa/kev, vuln-intel://first/epss, and
// vuln-intel://glad/<ecosystem>, plus vuln-intel://ghsa/<reviewed|malware>
// where the advisory type must be exactly one of those two segments with
// nothing after it (a '?' suffix is allowed, as for the other sources).
// Matching is exact and case-sensitive, the
// same way readiness compares scope ids. ok is false for any scope id outside
// that grammar, so a caller never invents a source token.
func SourceForScopeID(scopeID string) (string, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(scopeID), scopeIDPrefix)
	if !found {
		return "", false
	}
	authority, path := splitScopeSegment(rest)
	switch authority {
	case SourceOSV, SourceNVD, SourceGLAD:
		return authority, true
	case SourceGHSA:
		if segment, tail := splitScopeSegment(path); (segment == "reviewed" || segment == "malware") && tail == "" && !strings.Contains(path, "/") {
			return SourceGHSA, true
		}
	case "cisa":
		if segment, _ := splitScopeSegment(path); segment == "kev" {
			return SourceCISAKEV, true
		}
	case "first":
		if segment, _ := splitScopeSegment(path); segment == "epss" {
			return SourceFirstEPSS, true
		}
	case "first_epss":
		// The coordinator derives EPSS finding scopes under this authority
		// (vuln-intel://first_epss/_findings/<chunk>), distinct from the
		// configured vuln-intel://first/epss form (#10009).
		if segment, _ := splitScopeSegment(path); segment == "_findings" {
			return SourceFirstEPSS, true
		}
	}
	return "", false
}

// splitScopeSegment returns the leading path segment of value (up to the
// first '/' or '?') and what follows a '/' separator.
func splitScopeSegment(value string) (string, string) {
	end := strings.IndexAny(value, "/?")
	if end < 0 {
		return value, ""
	}
	if value[end] == '?' {
		return value[:end], ""
	}
	return value[:end], value[end+1:]
}
