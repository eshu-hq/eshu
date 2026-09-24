// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

import "net/http"

func scopedSecretsIAMRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/secrets-iam/identity-trust-chains",
		"/api/v0/secrets-iam/privilege-posture-observations",
		"/api/v0/secrets-iam/secret-access-paths",
		"/api/v0/secrets-iam/posture-gaps",
		"/api/v0/secrets-iam/posture-summary":
		return true
	default:
		return false
	}
}
