// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcpsetup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AuthPosture selects which credential story the emitted MCP client
// snippet wires: a per-user bearer token, an OAuth flow, or the legacy
// shared admin/dev key. The zero value is PostureToken, deliberately: any
// call site (including a test) that forgets to set Posture gets the safe
// per-user-token default, never the shared key.
type AuthPosture int

const (
	// PostureToken wires the per-user bearer token via ${ESHU_MCP_TOKEN}.
	// It is the default posture (zero value) and always authenticates,
	// since per-user tokens exist under every posture (issue #5169, F-8).
	PostureToken AuthPosture = iota
	// PostureSSO wires an OAuth flow via RFC 9728 discovery: the client
	// hits the endpoint unauthenticated, follows the 401 challenge, and
	// completes Authorization Code + PKCE against the deployment's IdP.
	PostureSSO
	// PostureSharedKey wires the legacy shared ${ESHU_API_KEY} admin/dev
	// credential. It is never the default; it is only selected by an
	// explicit --auth shared-key or --shared-key flag.
	PostureSharedKey
)

// postureExplicitValues lists the --auth values that resolve a posture without
// probing. It is the recovery advice an operator gets when auto-detection
// cannot run, so it deliberately omits "auto".
const postureExplicitValues = "sso, token, or shared-key"

// postureProbeAcceptedValues lists the --auth flag values ResolveAuthPosture
// accepts, used both for parsing and for the invalid-value error message.
const postureProbeAcceptedValues = "auto, " + postureExplicitValues

// oauthProtectedResourceProbeDoc is the subset of the RFC 9728 OAuth
// Protected Resource Metadata document (query.OAuthProtectedResourceMetadata)
// the CLI probe needs. It intentionally decodes only the fields the posture
// decision and SSO guidance notes consume, not the full server-side shape.
type oauthProtectedResourceProbeDoc struct {
	Resource                  string   `json:"resource"`
	AuthorizationServers      []string `json:"authorization_servers,omitempty"`
	EshuPreregisteredClientID string   `json:"eshu_preregistered_client_id,omitempty"`
}

// PostureProbeResult is the outcome of the RFC 9728 discovery probe (or of an
// explicit --auth/--shared-key resolution that skipped probing).
type PostureProbeResult struct {
	// Posture is the resolved credential story.
	Posture AuthPosture
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

// HostedPostureProbe adapts probeAuthPosture to the func(string)
// PostureProbeResult shape ResolveAuthPosture calls for "auto" in hosted
// mode, binding it to the dedicated short-timeout probe client.
func HostedPostureProbe(baseURL string) PostureProbeResult {
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
func probeAuthPosture(client *http.Client, baseURL string) PostureProbeResult {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	url := base + "/.well-known/oauth-protected-resource"

	resp, err := client.Get(url) // #nosec G107 -- url is operator-supplied --service-url, not request-controlled
	if err != nil {
		return PostureProbeResult{
			Posture: PostureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s failed: %v); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url, err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return PostureProbeResult{Posture: PostureToken}
	}
	if resp.StatusCode != http.StatusOK {
		return PostureProbeResult{
			Posture: PostureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s returned status %d); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url, resp.StatusCode),
		}
	}

	var doc oauthProtectedResourceProbeDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return PostureProbeResult{
			Posture: PostureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s returned malformed JSON: %v); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url, err),
		}
	}
	if len(doc.AuthorizationServers) == 0 {
		return PostureProbeResult{
			Posture: PostureToken,
			Warning: fmt.Sprintf("could not verify auth posture (probe %s returned no authorization servers); emitting per-user token config. If this deployment uses SSO for MCP, re-run with --auth sso.", url),
		}
	}

	return PostureProbeResult{
		Posture:               PostureSSO,
		Issuers:               doc.AuthorizationServers,
		PreregisteredClientID: doc.EshuPreregisteredClientID,
	}
}

// ResolveAuthPosture merges the --auth flag, the --shared-key boolean, and
// (for "auto" in hosted mode) the discovery probe into one posture decision.
//
// --shared-key wins unconditionally: it is the explicit legacy escape hatch
// and never probes. An explicit --auth value (sso, token, or shared-key)
// also never probes -- probe is only invoked for "auto" while hosted is
// true. Local stdio mode (hosted false) never probes regardless of --auth,
// since stdio mode carries no credential to select between. An unrecognized
// --auth value is an error listing the accepted values.
//
// probe is required for exactly one combination: sharedKey false, hosted true,
// and authFlag empty or "auto". That is the only branch that calls it, and a
// nil probe there is an error, not a panic. sharedKey true returns before the
// switch, so it needs no probe even in hosted "auto" mode. Every other
// combination ignores probe entirely, which is why the tests pass a probe that
// panics if called -- reaching it proves a no-probe path regressed. Callers
// that always probe should pass HostedPostureProbe.
func ResolveAuthPosture(authFlag string, sharedKey bool, hosted bool, probe func(string) PostureProbeResult, serviceURL string) (PostureProbeResult, error) {
	if sharedKey {
		return PostureProbeResult{Posture: PostureSharedKey}, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(authFlag))
	if normalized == "" {
		normalized = "auto"
	}

	switch normalized {
	case "sso":
		return PostureProbeResult{Posture: PostureSSO}, nil
	case "token":
		return PostureProbeResult{Posture: PostureToken}, nil
	case "shared-key":
		return PostureProbeResult{Posture: PostureSharedKey}, nil
	case "auto":
		if !hosted {
			return PostureProbeResult{Posture: PostureToken}, nil
		}
		if probe == nil {
			return PostureProbeResult{}, fmt.Errorf("cannot auto-detect the auth posture: hosted setup was started without a discovery probe; re-run with an explicit --auth value (%s)", postureExplicitValues)
		}
		return probe(serviceURL), nil
	default:
		return PostureProbeResult{}, fmt.Errorf("unsupported --auth value %q: expected %s", authFlag, postureProbeAcceptedValues)
	}
}
