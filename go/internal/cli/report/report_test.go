// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reportbundle"
)

// httpEnvelopeClient is a stand-in for the CLI's APIClient that speaks the same
// two envelope calls over real HTTP. Real HTTP matters here: a transport
// failure has to arrive as the *url.Error net/http actually produces, which is
// the shape requestErrorWithoutURL takes apart. cmd/eshu's own tests drive the
// production client through these same paths.
type httpEnvelopeClient struct {
	baseURL string
}

func (c *httpEnvelopeClient) do(method, path string, body, result any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader) //nolint:noctx // test client
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/eshu.envelope+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(raw, result)
	}
	return nil
}

func (c *httpEnvelopeClient) GetEnvelope(path string, result any) error {
	return c.do(http.MethodGet, path, nil, result)
}

func (c *httpEnvelopeClient) PostEnvelope(path string, body, result any) error {
	return c.do(http.MethodPost, path, body, result)
}

// canaryEnvelopeServer returns a canned query.ResponseEnvelope carrying a
// verbatim truth envelope plus a citation embedding an Excerpt (inline content
// bytes), so the assertions below can prove the truth envelope survives
// byte-for-byte and the excerpt never reaches a public-profile bundle.
func canaryEnvelopeServer(t *testing.T, wantPath string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantPath != "" && r.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{
			"data": {
				"owner": "platform-team",
				"truncated": true,
				"citations": [{"repo_id": "demo/service", "relative_path": "main.go", "excerpt": "func Handler() { return nil }"}]
			},
			"truth": {
				"level": "exact",
				"capability": "trace.service_story",
				"profile": "local_authoritative",
				"basis": "authoritative_graph",
				"backend": "nornicdb",
				"freshness": {"state": "fresh"}
			},
			"error": null
		}`))
	}))
}

// TestCaptureBundle_AgainstEnvelopeServer proves CaptureBundle fetches the
// envelope, stores the query.TruthEnvelope verbatim, records the observed
// truncation flag, drops the embedded citation excerpt, and produces a bundle
// that passes its own Validate gate.
func TestCaptureBundle_AgainstEnvelopeServer(t *testing.T) {
	t.Parallel()

	server := canaryEnvelopeServer(t, "/api/v0/services/checkout/story")
	defer server.Close()

	result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{
		Endpoint:   "/api/v0/services/checkout/story",
		ParamsJSON: `{"repo":"demo/service","api_key":"sk-live-should-not-leak"}`,
		Note:       "expected the owning team, got an empty list",
	})
	if err != nil {
		t.Fatalf("CaptureBundle() error = %v, want nil", err)
	}

	bundle := result.Bundle
	if bundle.SchemaVersion != reportbundle.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", bundle.SchemaVersion, reportbundle.SchemaVersion)
	}
	if bundle.Response.Truth == nil {
		t.Fatalf("Response.Truth is nil, want the verbatim truth envelope")
	}
	if bundle.Response.Truth.Level != "exact" || bundle.Response.Truth.Backend != "nornicdb" {
		t.Fatalf("Response.Truth = %+v, want verbatim server truth envelope", bundle.Response.Truth)
	}
	if !bundle.Response.Truncated {
		t.Fatalf("Response.Truncated = false, want true (observed from response data)")
	}
	if bundle.Query.Profile != "local_authoritative" {
		t.Fatalf("Query.Profile = %q, want the profile the truth envelope reported", bundle.Query.Profile)
	}
	if bundle.Redaction.Profile != reportbundle.ProfilePublic {
		t.Fatalf("Redaction.Profile = %q, want %q", bundle.Redaction.Profile, reportbundle.ProfilePublic)
	}

	raw := string(result.JSON)
	if strings.Contains(raw, "sk-live-should-not-leak") {
		t.Fatalf("captured bundle leaks the api_key sentinel value:\n%s", raw)
	}
	if strings.Contains(raw, "\"excerpt\":") {
		t.Fatalf("captured bundle carries a live excerpt key:\n%s", raw)
	}
	if !strings.HasSuffix(raw, "}\n") {
		t.Fatalf("CaptureResult.JSON is not newline-terminated:\n%q", raw[max(0, len(raw)-20):])
	}

	if err := reportbundle.Validate(bundle, reportbundle.ValidateOptions{RequirePublic: true}); err != nil {
		t.Fatalf("Validate(bundle, RequirePublic=true) error = %v, want nil", err)
	}
}

// TestCaptureBundle_GETMergesEndpointQueryWithParams pins the request URL
// fetchEnvelope builds. The other tests all pass an endpoint with no query
// string, so none of them reaches the merge loop, the repeated-value branch, or
// the collision rule — a regression there would build a wrong request and,
// because Capture splits the same target, record parameters that no longer
// describe it.
//
// It also pins the deliberate asymmetry: the request keeps the credential (it
// is the reporter's own API call, and the answer under investigation is the one
// that credential returns), the bundle does not.
func TestCaptureBundle_GETMergesEndpointQueryWithParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.URL.Path != "/api/v0/services/checkout/story" {
			t.Errorf("request path = %q, want the bare path with the query string split off", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{
		Endpoint:   "/api/v0/services/checkout/story?repo=demo%2Fservice&tag=alpha&tag=beta&limit=5&api_key=sk-live-should-not-leak",
		ParamsJSON: `{"limit":25}`,
	})
	if err != nil {
		t.Fatalf("CaptureBundle() error = %v, want nil", err)
	}

	// The request the server actually received.
	if got := gotQuery.Get("repo"); got != "demo/service" {
		t.Errorf("request repo = %q, want the endpoint parameter merged in", got)
	}
	if got := gotQuery["tag"]; len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("request tag = %#v, want both repeated endpoint values in order", got)
	}
	if got := gotQuery["limit"]; len(got) != 1 || got[0] != "25" {
		t.Errorf("request limit = %#v, want the explicit params value to replace the endpoint's, not append to it", got)
	}
	if got := gotQuery.Get("api_key"); got != "sk-live-should-not-leak" {
		t.Errorf("request api_key = %q, want the reporter's own credential still sent with the query under investigation", got)
	}

	// The bundle recorded alongside it.
	bundle := result.Bundle
	if bundle.Query.Target != "/api/v0/services/checkout/story" {
		t.Errorf("Query.Target = %q, want the bare path", bundle.Query.Target)
	}
	if got := bundle.Query.Params["repo"]; got != "demo/service" {
		t.Errorf("Query.Params[\"repo\"] = %#v, want the endpoint parameter recorded", got)
	}
	tags, ok := bundle.Query.Params["tag"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Errorf("Query.Params[\"tag\"] = %#v, want both repeated values recorded in order", bundle.Query.Params["tag"])
	}
	if got := bundle.Query.Params["limit"]; got != float64(25) {
		t.Errorf("Query.Params[\"limit\"] = %#v, want the explicit params value, matching what was issued", got)
	}
	if _, present := bundle.Query.Params["api_key"]; present {
		t.Errorf("Query.Params carries api_key; the request may send it, the shared artifact may not")
	}
	if strings.Contains(string(result.JSON), "sk-live-should-not-leak") {
		t.Errorf("captured bundle leaks the endpoint credential:\n%s", result.JSON)
	}
}

// TestCaptureBundle_RefusesUnparseableEndpointQueryString proves capture fails
// closed on an endpoint whose query string net/url cannot parse, instead of
// issuing a request with the query silently emptied and returning a bundle that
// still carries the raw target.
func TestCaptureBundle_RefusesUnparseableEndpointQueryString(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server was called; capture must refuse the malformed endpoint before issuing the request")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{
		Endpoint: "/api/v0/services/checkout/story?api_key=sk-live-should-not-leak&bad=%ZZ",
	})
	if err == nil {
		t.Fatalf("CaptureBundle() error = nil, want refusal; bundle = %s", result.JSON)
	}
	if strings.Contains(err.Error()+string(result.JSON), "sk-live-should-not-leak") {
		t.Errorf("refused capture echoed the credential:\nerr = %v\nbundle = %s", err, result.JSON)
	}
}

// TestCaptureBundle_UnsupportedMethod covers the one branch of fetchEnvelope
// neither GET nor POST reaches, and the default that turns an empty method into
// GET.
func TestCaptureBundle_UnsupportedMethod(t *testing.T) {
	t.Parallel()

	server := canaryEnvelopeServer(t, "")
	defer server.Close()
	client := &httpEnvelopeClient{baseURL: server.URL}

	if _, err := CaptureBundle(client, CaptureOptions{Endpoint: "/api/v0/x", Method: "DELETE"}); err == nil {
		t.Fatalf("CaptureBundle(method=DELETE) error = nil, want a refusal")
	} else if !strings.Contains(err.Error(), "unsupported --method") {
		t.Errorf("error = %v, want it to name the unsupported method", err)
	}

	for _, method := range []string{"", "  ", "get", "Get"} {
		result, err := CaptureBundle(client, CaptureOptions{Endpoint: "/api/v0/x", Method: method})
		if err != nil {
			t.Fatalf("CaptureBundle(method=%q) error = %v, want nil", method, err)
		}
		if result.Bundle.Query.Method != http.MethodGet {
			t.Errorf("Query.Method = %q for method=%q, want %q", result.Bundle.Query.Method, method, http.MethodGet)
		}
	}
}

// TestCaptureBundle_POSTSendsParamsAsBody covers the POST branch, whose recorded
// target is split the same way the GET branch's is.
func TestCaptureBundle_POSTSendsParamsAsBody(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{"owner":"platform-team"},"truth":{"level":"exact","profile":"local_authoritative"},"error":null}`))
	}))
	defer server.Close()

	result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{
		Endpoint:   "/api/v0/query",
		Method:     "post",
		ParamsJSON: `{"repo":"demo/service"}`,
	})
	if err != nil {
		t.Fatalf("CaptureBundle() error = %v, want nil", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotMethod)
	}
	if gotBody["repo"] != "demo/service" {
		t.Errorf("request body = %#v, want the params sent as the body", gotBody)
	}
	if result.Bundle.Query.Method != http.MethodPost {
		t.Errorf("Query.Method = %q, want POST", result.Bundle.Query.Method)
	}
}

// TestCaptureBundle_RejectsNonObjectParams pins the --params contract: the flag
// takes a JSON object, and anything else is refused before a request goes out.
func TestCaptureBundle_RejectsNonObjectParams(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server was called; capture must refuse malformed --params first")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for _, params := range []string{`["a"]`, `"a"`, `{`} {
		_, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{
			Endpoint:   "/api/v0/x",
			ParamsJSON: params,
		})
		if err == nil {
			t.Errorf("CaptureBundle(params=%q) error = nil, want a refusal", params)
			continue
		}
		if !strings.Contains(err.Error(), "--params must be a JSON object") {
			t.Errorf("CaptureBundle(params=%q) error = %v, want it to name --params", params, err)
		}
	}
}

// TestCaptureBundle_IncludePayloadsFlipsProfile proves --include-payloads
// produces a private-triage bundle that a subsequent --require-public check
// rejects.
func TestCaptureBundle_IncludePayloadsFlipsProfile(t *testing.T) {
	t.Parallel()

	server := canaryEnvelopeServer(t, "")
	defer server.Close()

	result, err := CaptureBundle(&httpEnvelopeClient{baseURL: server.URL}, CaptureOptions{
		Endpoint:        "/api/v0/services/checkout/story",
		IncludePayloads: true,
	})
	if err != nil {
		t.Fatalf("CaptureBundle() error = %v, want nil", err)
	}
	if result.Bundle.Redaction.Profile != reportbundle.ProfilePrivateTriage {
		t.Fatalf("Redaction.Profile = %q, want %q", result.Bundle.Redaction.Profile, reportbundle.ProfilePrivateTriage)
	}
	if err := reportbundle.Validate(result.Bundle, reportbundle.ValidateOptions{RequirePublic: true}); err == nil {
		t.Fatalf("Validate(bundle, RequirePublic=true) error = nil, want rejection of a private-triage bundle")
	}
	if !strings.Contains(IncludePayloadsWarning, "PRIVATE TRIAGE ONLY") {
		t.Errorf("IncludePayloadsWarning lost its headline:\n%s", IncludePayloadsWarning)
	}
}

// TestRequestErrorWithoutURLNeverEchoesParseInput plants a credential sentinel
// where net/url quotes input into the NESTED error of a parse-shaped
// *url.Error — the invalid port. Stripping the outer envelope is not enough:
// `invalid port ":secret" after host` repeats the input inside urlErr.Err, and
// this message reaches stderr and CI logs. The transport case below is the
// positive control for the other branch: a genuine transport failure must keep
// its inner error wrapped, so errors.As still classifies it.
func TestRequestErrorWithoutURLNeverEchoesParseInput(t *testing.T) {
	t.Parallel()

	const sentinel = "PORT-CRED-SENTINEL-6140"

	// The real parse error net/http produces for an unparseable request URL,
	// wrapped the way APIClient.do wraps it.
	_, err := http.NewRequest(http.MethodGet, "https://h.internal:"+sentinel+"/api/v0/x", nil) //nolint:noctx // never sent
	if err == nil {
		t.Fatal("http.NewRequest() error = nil, want a parse failure for an invalid port")
	}
	got := requestErrorWithoutURL(fmt.Errorf("create request: %w", err), "/api/v0/x")
	if got == nil {
		t.Fatal("requestErrorWithoutURL() = nil, want an error")
	}
	if strings.Contains(got.Error(), sentinel) {
		t.Errorf("requestErrorWithoutURL() = %q repeats the request URL's port; these messages reach stderr and CI logs", got)
	}
	if !strings.Contains(got.Error(), "invalid port") {
		t.Errorf("requestErrorWithoutURL() = %q lost the parse reason; a reader needs to know why the request never went out", got)
	}

	// Positive control: a real transport error keeps its cause wrapped.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := server.URL
	server.Close()
	resp, err := http.Get(deadURL + "/api/v0/x") //nolint:noctx,bodyclose // the request must fail
	if err == nil {
		resp.Body.Close()
		t.Fatal("http.Get() error = nil, want a transport failure against a closed server")
	}
	got = requestErrorWithoutURL(err, "/api/v0/x")
	var opErr *net.OpError
	if !errors.As(got, &opErr) {
		t.Errorf("requestErrorWithoutURL() = %q no longer wraps the transport cause; errors.As lost the *net.OpError", got)
	}
	if strings.Contains(got.Error(), deadURL) {
		t.Errorf("requestErrorWithoutURL() = %q repeats the request URL", got)
	}
}
