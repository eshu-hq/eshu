// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

func serveRequest(t *testing.T, method, target string, body []byte, accept string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&Handlers{}).Mount(mux)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// samplePacket is a small deterministic investigation packet exercising every
// rendered layer the endpoint must preserve byte-for-byte.
func samplePacket() query.InvestigationEvidencePacket {
	return query.InvestigationEvidencePacket{
		Schema:   query.InvestigationEvidencePacketSchema,
		PacketID: "packet-test-001",
		Identity: query.PacketIdentity{
			Family:  query.InvestigationFamilySupplyChainImpact,
			Subject: map[string]string{"finding_id": "F-1"},
			Basis:   query.PacketBasisDeterministic,
		},
		Truth: &query.TruthEnvelope{
			Level:     query.TruthLevelExact,
			Basis:     query.TruthBasisAuthoritativeGraph,
			Freshness: query.TruthFreshness{State: query.FreshnessFresh},
		},
		Freshness: query.TruthFreshness{State: query.FreshnessFresh},
	}
}

// TestRenderEndpointMatchesLocalRender is the S4 parity proof for packet
// rendering: the endpoint returns byte-identical output to the local
// RenderInvestigationPacket for the same input, in every format.
func TestRenderEndpointMatchesLocalRender(t *testing.T) {
	t.Parallel()
	for _, format := range []query.InvestigationPacketFormat{
		query.InvestigationPacketFormatJSON,
		query.InvestigationPacketFormatMarkdown,
		query.InvestigationPacketFormatHTML,
	} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			packet := samplePacket()
			want, err := query.RenderInvestigationPacket(packet, format)
			if err != nil {
				t.Fatalf("local render: %v", err)
			}
			rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
				mustJSON(t, RenderRequest{Packet: packet, Format: string(format)}), "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Body.Bytes(); !bytes.Equal(got, want) {
				t.Fatalf("endpoint render differs from local:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// TestRenderEndpointRejectsUnknownFormat pins the 400 contract for a format
// the local renderer refuses.
func TestRenderEndpointRejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
		mustJSON(t, RenderRequest{Packet: samplePacket(), Format: "pdf"}), query.EnvelopeMIMEType)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != query.ErrorCodeInvalidArgument {
		t.Fatalf("error = %+v, want invalid_argument", envelope.Error)
	}
}

// oversizedValidJSON returns valid JSON larger than cap: a bare oversized
// blob fails on syntax before the cap trips, so the 413 path needs a value
// the decoder keeps reading past the limit.
func oversizedValidJSON(cap int) []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"pad":"`)
	buf.Write(bytes.Repeat([]byte("z"), cap+1))
	buf.WriteString(`"}`)
	return buf.Bytes()
}

// TestRenderEndpointRejectsOversizeBody pins the fail-closed body cap.
func TestRenderEndpointRejectsOversizeBody(t *testing.T) {
	t.Parallel()
	rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
		oversizedValidJSON(maxRenderBodyBytes), query.EnvelopeMIMEType)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

// sampleDossier is a minimal captured service-story dossier with one
// identity evidence section.
func sampleDossier() (map[string]any, *query.TruthEnvelope) {
	return map[string]any{
			"service": map[string]any{"name": "checkout", "id": "svc-1"},
		}, &query.TruthEnvelope{
			Level:     query.TruthLevelExact,
			Basis:     query.TruthBasisAuthoritativeGraph,
			Freshness: query.TruthFreshness{State: query.FreshnessFresh},
		}
}

// TestComposeEndpointMatchesLocalCompose is the S4 parity proof for service
// intelligence composition.
func TestComposeEndpointMatchesLocalCompose(t *testing.T) {
	t.Parallel()
	dossier, truth := sampleDossier()
	want := serviceintel.Compose(serviceintel.FromServiceStory(dossier, truth))
	rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose",
		mustJSON(t, ComposeRequest{Dossier: dossier, Truth: truth}), query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	gotRaw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("re-marshal data: %v", err)
	}
	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	// Compare canonically: the endpoint path round-trips the report through
	// an any-typed envelope, which reorders object keys. Byte order is not
	// the contract; the decoded value is.
	var gotAny, wantAny any
	if err := json.Unmarshal(gotRaw, &gotAny); err != nil {
		t.Fatalf("decode got: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &wantAny); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Fatalf("endpoint compose differs from local:\n got: %s\nwant: %s", gotRaw, wantRaw)
	}
}

// sampleCaptureInput is a credential-free capture over a small envelope.
func sampleCaptureInput() reportbundle.CaptureInput {
	return reportbundle.CaptureInput{
		Surface: "api",
		Target:  "/api/v0/search/code",
		Method:  "GET",
		Params:  map[string]any{"q": "auth middleware"},
		Profile: "production",
		Envelope: query.ResponseEnvelope{
			Data: map[string]any{"repositories": []any{}},
			Truth: &query.TruthEnvelope{
				Level:     query.TruthLevelDerived,
				Basis:     query.TruthBasisHybrid,
				Freshness: query.TruthFreshness{State: query.FreshnessFresh},
			},
		},
		ReporterNote: "expected the auth middleware repo",
	}
}

func normalizeBundleTimes(b *reportbundle.Bundle) {
	b.CreatedAt = ""
	b.BundleID = ""
}

// TestCaptureEndpointMatchesLocalCapture is the S4 parity proof for bundle
// capture. CreatedAt/BundleID vary by design (distinct ids per capture), so
// the test normalizes those two fields and requires everything else
// byte-identical, with both ids non-empty.
func TestCaptureEndpointMatchesLocalCapture(t *testing.T) {
	t.Parallel()
	input := sampleCaptureInput()
	want, err := reportbundle.Capture(input)
	if err != nil {
		t.Fatalf("local capture: %v", err)
	}
	rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
		mustJSON(t, input), query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("re-marshal data: %v", err)
	}
	var got reportbundle.Bundle
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if got.BundleID == "" || want.BundleID == "" {
		t.Fatalf("empty bundle id: got %q want %q", got.BundleID, want.BundleID)
	}
	normalizeBundleTimes(&got)
	normalizeBundleTimes(&want)
	gotRaw, _ := json.Marshal(got)
	wantRaw, _ := json.Marshal(want)
	if string(gotRaw) != string(wantRaw) {
		t.Fatalf("endpoint capture differs from local:\n got: %s\nwant: %s", gotRaw, wantRaw)
	}
}

// TestCaptureEndpointRefusesCredentialTarget pins the fail-closed contract:
// a target carrying authority userinfo is refused with 422 and the secret
// never appears in the response. It mirrors the CLI credential canary at
// the wire.
func TestCaptureEndpointRefusesCredentialTarget(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-canary-value"
	input := sampleCaptureInput()
	input.Target = "https://svc:" + secret + "@h.internal/tool"
	rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
		mustJSON(t, input), query.EnvelopeMIMEType)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("response leaks the planted credential")
	}
}

// TestCaptureEndpointRedactsCredentialParams documents the other half of the
// redact-vs-refuse boundary: key=value credentials in params are captured
// with redaction (200), while authority userinfo is refused (above).
func TestCaptureEndpointRedactsCredentialParams(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-param-value"
	input := sampleCaptureInput()
	input.Target = "/api/v0/search/code"
	input.Params = map[string]any{"api_key": secret, "q": "auth middleware"}
	rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
		mustJSON(t, input), query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("response leaks the planted param credential")
	}
}

// TestCaptureEndpointRejectsOversizeBody pins the fail-closed body cap for
// the payload-bearing private-triage mode.
func TestCaptureEndpointRejectsOversizeBody(t *testing.T) {
	t.Parallel()
	rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
		oversizedValidJSON(maxCaptureBodyBytes), query.EnvelopeMIMEType)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

// TestValidateEndpointVerdict pins the 200-always contract: an invalid
// bundle yields valid:false (not a transport error), and a locally captured
// bundle validates clean through the endpoint.
func TestValidateEndpointVerdict(t *testing.T) {
	t.Parallel()
	bundle, err := reportbundle.Capture(sampleCaptureInput())
	if err != nil {
		t.Fatalf("local capture: %v", err)
	}
	rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/validate",
		mustJSON(t, ValidateRequest{Bundle: bundle}), query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid bundle status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, _ := json.Marshal(envelope.Data)
	var verdict ValidateVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		t.Fatalf("decode verdict: %v", err)
	}
	if !verdict.Valid {
		t.Fatalf("locally captured bundle validates invalid: %+v", verdict)
	}

	bundle.Query.Target = "/api/v0/search/code?api_key=planted"
	rec = serveRequest(t, http.MethodPost, "/api/v0/report/bundle/validate",
		mustJSON(t, ValidateRequest{Bundle: bundle, RequirePublic: true}), query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid bundle status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, _ = json.Marshal(envelope.Data)
	if err := json.Unmarshal(raw, &verdict); err != nil {
		t.Fatalf("decode verdict: %v", err)
	}
	if verdict.Valid {
		t.Fatalf("credential-bearing bundle validates valid: %+v", verdict)
	}
	if strings.Contains(rec.Body.String(), "planted") {
		t.Fatalf("validate response echoes the planted credential")
	}
}

// sampleEvidence is a minimal deterministic scorecard input.
func sampleEvidence() answerquality.Evidence {
	return answerquality.Evidence{
		Version: answerquality.EvidenceVersion,
		RunID:   "s4-test",
		Prompts: []answerquality.PromptResult{
			{
				ID:                 "p1",
				Family:             answerquality.PromptFamilyCodeTopic,
				Prompt:             "where is auth enforced",
				ExpectedTruthClass: "deterministic",
				Results:            []answerquality.SurfaceResult{},
			},
		},
	}
}

// TestScoreEndpointMatchesLocalScore is the S4 parity proof for answer
// quality scoring.
func TestScoreEndpointMatchesLocalScore(t *testing.T) {
	t.Parallel()
	evidence := sampleEvidence()
	want := answerquality.Score(evidence)
	rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
		mustJSON(t, evidence), query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	gotRaw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("re-marshal data: %v", err)
	}
	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	// Canonical comparison: the envelope round-trip reorders object keys.
	var gotAny, wantAny any
	if err := json.Unmarshal(gotRaw, &gotAny); err != nil {
		t.Fatalf("decode got: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &wantAny); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Fatalf("endpoint score differs from local:\n got: %s\nwant: %s", gotRaw, wantRaw)
	}
}
