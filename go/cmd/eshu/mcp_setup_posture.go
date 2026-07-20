// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// mcpAuthPosture selects which credential story the emitted MCP client
// snippet wires: a per-user bearer token, an OAuth flow, or the legacy
// shared admin/dev key. The zero value is postureToken, deliberately: any
// call site (including a test) that forgets to set Posture gets the safe
// per-user-token default, never the shared key.
type mcpAuthPosture int

const (
	// postureToken wires the per-user bearer token via ${ESHU_MCP_TOKEN}.
	// It is the default posture (zero value) and always authenticates,
	// since per-user tokens exist under every posture (issue #5169, F-8).
	postureToken mcpAuthPosture = iota
	// postureSSO wires an OAuth flow via RFC 9728 discovery: the client
	// hits the endpoint unauthenticated, follows the 401 challenge, and
	// completes Authorization Code + PKCE against the deployment's IdP.
	postureSSO
	// postureSharedKey wires the legacy shared ${ESHU_API_KEY} admin/dev
	// credential. It is never the default; it is only selected by an
	// explicit --auth shared-key or --shared-key flag.
	postureSharedKey
)

// postureProbeAcceptedValues lists the --auth flag values resolveAuthPosture
// accepts, used both for parsing and for the invalid-value error message.
const postureProbeAcceptedValues = "auto, sso, token, or shared-key"

// oauthProtectedResourceProbeDoc is the subset of the RFC 9728 OAuth
// Protected Resource Metadata document (query.OAuthProtectedResourceMetadata)
// the CLI probe needs. It intentionally decodes only the fields the posture
// decision and SSO guidance notes consume, not the full server-side shape.
type oauthProtectedResourceProbeDoc struct {
	Resource                  string   `json:"resource"`
	AuthorizationServers      []string `json:"authorization_servers,omitempty"`
	EshuPreregisteredClientID string   `json:"eshu_preregistered_client_id,omitempty"`
}

// postureProbeResult is the outcome of the RFC 9728 discovery probe (or of an
// explicit --auth/--shared-key resolution that skipped probing).
type postureProbeResult struct {
	// Posture is the resolved credential story.
	Posture mcpAuthPosture
	// Issuers holds authorization_servers from a 200 probe response; it
	// names the IdP in SSO-posture guidance notes.
	Issuers []string
	// PreregisteredClientID is eshu_preregistered_client_id from a 200 probe
	// response, when the deployment advertises one.
	PreregisteredClientID string
	// Warning is non-empty when auto-detection could not positively confirm
	// SSO and fell back to token posture. Empty on an explicit resolution or
	// on a clean 404 (the F-2-documented "no SSO here" signal).
	Warning string
}

// newPostureProbeClient returns a dedicated short-timeout HTTP client for the
// auth-posture probe. An offline `eshu mcp setup --hosted` run must not hang
// for the APIClient's 30s default, so this is a separate 3s-timeout client,
// never a reused APIClient.HTTPClient.
func newPostureProbeClient() *http.Client {
	return &http.Client{Timeout: 3 * time.Second}
}

// hostedPostureProbe adapts probeAuthPosture to the func(string)
// postureProbeResult shape resolveAuthPosture calls for "auto" in hosted
// mode, binding it to the dedicated short-timeout probe client.
func hostedPostureProbe(baseURL string) postureProbeResult {
	return probeAuthPosture(newPostureProbeClient(), baseURL)
}

// probeAuthPosture GETs {baseURL}/.well-known/oauth-protected-resource with
// client and maps the outcome per the F-2 discovery contract
// (go/internal/query/auth_oauth_discovery.go): 200 with a non-empty
// authorization_servers list proves the OAuth flow can complete, so that maps
// to SSO. Every other outcome -- 404 (the documented "no active bearer
// issuer" signal), a non-200/404 status, a network error, a timeout,
// malformed JSON, or (defensively) a 200 with an empty issuer list the server
// should never send -- maps to token posture. Per-user tokens authenticate
// under every posture, so a misdetection can only ever fall toward a
// configuration that still works; the reverse (emitting an OAuth-only
// config against a token-only deployment) never happens from auto-detect,
// only from an explicit --auth sso.
func probeAuthPosture(client *http.Client, baseURL string) postureProbeResult {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	url := base + "/.well-known/oauth-protected-resource"

	resp, err := client.Get(url) // #nosec G107 -- url is operator-supplied --service-url, not request-controlled
	if err != nil {
		return postureProbeResult{
			Posture: postureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s failed: %v); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url, err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return postureProbeResult{Posture: postureToken}
	}
	if resp.StatusCode != http.StatusOK {
		return postureProbeResult{
			Posture: postureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s returned status %d); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url, resp.StatusCode),
		}
	}

	var doc oauthProtectedResourceProbeDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return postureProbeResult{
			Posture: postureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s returned malformed JSON: %v); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url, err),
		}
	}
	if len(doc.AuthorizationServers) == 0 {
		return postureProbeResult{
			Posture: postureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s returned no authorization servers); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url),
		}
	}

	return postureProbeResult{
		Posture:               postureSSO,
		Issuers:               doc.AuthorizationServers,
		PreregisteredClientID: doc.EshuPreregisteredClientID,
	}
}

// resolveAuthPosture merges the --auth flag, the --shared-key boolean, and
// (for "auto" in hosted mode) the discovery probe into one posture decision.
//
// --shared-key wins unconditionally: it is the explicit legacy escape hatch
// and never probes. An explicit --auth value (sso, token, or shared-key)
// also never probes -- probe is only invoked for "auto" while hosted is
// true. Local stdio mode (hosted false) never probes regardless of --auth,
// since stdio mode carries no credential to select between. An unrecognized
// --auth value is an error listing the accepted values.
func resolveAuthPosture(authFlag string, sharedKey bool, hosted bool, probe func(string) postureProbeResult, serviceURL string) (postureProbeResult, error) {
	if sharedKey {
		return postureProbeResult{Posture: postureSharedKey}, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(authFlag))
	if normalized == "" {
		normalized = "auto"
	}

	switch normalized {
	case "sso":
		return postureProbeResult{Posture: postureSSO}, nil
	case "token":
		return postureProbeResult{Posture: postureToken}, nil
	case "shared-key":
		return postureProbeResult{Posture: postureSharedKey}, nil
	case "auto":
		if !hosted {
			return postureProbeResult{Posture: postureToken}, nil
		}
		return probe(serviceURL), nil
	default:
		return postureProbeResult{}, fmt.Errorf("unsupported --auth value %q: expected %s", authFlag, postureProbeAcceptedValues)
	}
}
