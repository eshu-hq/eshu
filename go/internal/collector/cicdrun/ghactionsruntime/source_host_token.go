// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// hostTokenFilePayload is the arbiter-ruled host token file shape (#9182,
// #9186): an object carrying the minted Bearer and the scope it was minted
// for, the same shape as stdio config.host_github_app_token. The satellite
// never sees the App private key; it only spends the Bearer.
type hostTokenFilePayload struct {
	Token   string `json:"token"`
	ScopeID string `json:"scope_id"`
}

// ResolveHostTokenFile reads one host token file into the Bearer the claim
// spends. It is stateless by design: no cache, no lock, no background
// refresh. The host refreshes the file on a cadence below the 1-hour
// installation-token TTL by atomic replace (temp file plus rename), so each
// call observes either the old or the new complete file, never a partial
// one. Callers resolve per claim, immediately before spending.
//
// Every failure is a bounded terminal auth_denied: a missing file, an empty
// token, malformed JSON, or a scope_id that does not equal the target's
// ScopeID fails the claim without retry. The returned error never carries
// the token value.
func ResolveHostTokenFile(path, scopeID string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return "", hostTokenFailure("host token file path is required")
	}
	raw, err := os.ReadFile(trimmedPath)
	if err != nil {
		return "", hostTokenFailure(fmt.Sprintf("host token file %q is not readable", trimmedPath))
	}
	var payload hostTokenFilePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", hostTokenFailure(fmt.Sprintf("host token file %q is not valid token JSON", trimmedPath))
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		return "", hostTokenFailure(fmt.Sprintf("host token file %q holds no token", trimmedPath))
	}
	if strings.TrimSpace(payload.ScopeID) != strings.TrimSpace(scopeID) {
		return "", hostTokenFailure(fmt.Sprintf("host token file %q scope_id does not match target scope_id", trimmedPath))
	}
	return token, nil
}

// hostTokenFailure maps a token-resolution fault onto the bounded claim
// failure vocabulary without carrying credentials: an unreadable file, a
// revoked or rotated-away mint, or a misconfiguration is a terminal denial
// (no retry storm, no attempt-budget burn).
func hostTokenFailure(message string) error {
	return sdk.NewProviderFailure(
		string(cicdrun.ProviderGitHubActions),
		sdk.FailureAuthDenied,
		true,
		errors.New(message),
	).WithOperation("resolve_host_token")
}
