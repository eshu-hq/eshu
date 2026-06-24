// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func admissionDecisionsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/evidence/admission-decisions", query: map[string]string{
		"anchor_id":        str(args, "anchor_id"),
		"anchor_kind":      str(args, "anchor_kind"),
		"domain":           str(args, "domain"),
		"generation_id":    str(args, "generation_id"),
		"include_evidence": strconv.FormatBool(boolOr(args, "include_evidence", false)),
		"limit":            strconv.Itoa(intOr(args, "limit", 50)),
		"scope_id":         str(args, "scope_id"),
		"state":            str(args, "state"),
	}}
}
