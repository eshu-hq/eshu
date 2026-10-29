// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// withUnknownField injects one undeclared field into an encoded request so
// the unknown-field tests pin the DisallowUnknownFields contract without
// hand-writing JSON per route.
func withUnknownField(t *testing.T, raw []byte) []byte {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	obj["zzz_undeclared_field"] = 1
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("re-encode request: %v", err)
	}
	return out
}

// withTrailingValue appends a second JSON value so the trailing-value tests
// pin the single-value contract.
func withTrailingValue(t *testing.T, v any) []byte {
	t.Helper()
	return append(mustJSON(t, v), " {}"...)
}

// requireBadRequest pins the 400 + invalid_argument envelope every
// request-shape refusal must answer with.
func requireBadRequest(t *testing.T, body []byte, code int, route string) {
	t.Helper()
	if code != http.StatusBadRequest {
		t.Fatalf("%s: status = %d, want 400; body=%s", route, code, body)
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("%s: decode error envelope: %v", route, err)
	}
	if envelope.Error == nil || envelope.Error.Code != query.ErrorCodeInvalidArgument {
		t.Fatalf("%s: error = %+v, want invalid_argument", route, envelope.Error)
	}
}

// TestRenderEndpointSetsContentTypePerFormat pins the documented content
// type per render format. Swapping the md/html content-type lines passes a
// byte-only comparison, so the header needs its own assertion.
func TestRenderEndpointSetsContentTypePerFormat(t *testing.T) {
	t.Parallel()
	packet := samplePacket()
	for format, wantType := range map[query.InvestigationPacketFormat]string{
		query.InvestigationPacketFormatJSON:     "application/json; charset=utf-8",
		query.InvestigationPacketFormatMarkdown: "text/markdown; charset=utf-8",
		query.InvestigationPacketFormatHTML:     "text/html; charset=utf-8",
	} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
				mustJSON(t, RenderRequest{Packet: packet, Format: string(format)}), query.EnvelopeMIMEType)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != wantType {
				t.Fatalf("Content-Type = %q, want %q", got, wantType)
			}
		})
	}
}

// TestRenderEndpointEmptyFormatDefaultsJSON pins the empty-format default:
// an omitted format renders JSON, byte-identical to the explicit request.
func TestRenderEndpointEmptyFormatDefaultsJSON(t *testing.T) {
	t.Parallel()
	packet := samplePacket()
	implicit := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
		mustJSON(t, RenderRequest{Packet: packet}), query.EnvelopeMIMEType)
	if implicit.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", implicit.Code, implicit.Body.String())
	}
	if got := implicit.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	explicit := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
		mustJSON(t, RenderRequest{Packet: packet, Format: "json"}), query.EnvelopeMIMEType)
	if !bytes.Equal(implicit.Body.Bytes(), explicit.Body.Bytes()) {
		t.Fatal("empty format renders differently from explicit json")
	}
}

// TestRenderEndpointRejectsMissingPacket pins the required-packet contract:
// a missing or empty packet fails with 400 instead of rendering a blank.
func TestRenderEndpointRejectsMissingPacket(t *testing.T) {
	t.Parallel()
	for name, body := range map[string][]byte{
		"empty object": []byte(`{}`),
		"format only":  mustJSON(t, RenderRequest{Format: "json"}),
		"null packet":  []byte(`{"packet":null}`),
		"empty packet": []byte(`{"packet":{}}`),
		"nil body":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render", body, query.EnvelopeMIMEType)
			requireBadRequest(t, rec.Body.Bytes(), rec.Code, "render "+name)
		})
	}
}

// TestRenderEndpointRejectsBadShapes pins unknown-field, trailing-value,
// and malformed-JSON refusal on the render route.
func TestRenderEndpointRejectsBadShapes(t *testing.T) {
	t.Parallel()
	packet := samplePacket()
	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		body := withUnknownField(t, mustJSON(t, RenderRequest{Packet: packet}))
		rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render", body, query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "render unknown field")
	})
	t.Run("trailing value", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
			withTrailingValue(t, RenderRequest{Packet: packet}), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "render trailing value")
	})
	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/investigation/packet/render",
			[]byte(`{"packet":`), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "render malformed json")
	})
}

// TestComposeEndpointRequiresDossier pins the required-dossier contract: the
// offline adapter turns a nil dossier into an empty report that always
// composes, so the endpoint must refuse it with 400.
func TestComposeEndpointRequiresDossier(t *testing.T) {
	t.Parallel()
	for name, body := range map[string][]byte{
		"empty object": []byte(`{}`),
		"null dossier": []byte(`{"dossier":null}`),
		"nil body":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose", body, query.EnvelopeMIMEType)
			requireBadRequest(t, rec.Body.Bytes(), rec.Code, "compose "+name)
		})
	}
}

// TestComposeEndpointMatchesLocalComposeWithSupplyChain pins the
// supply-chain branch of the compose parity proof: dropping the
// FromSupplyChainInventory section still passes the dossier-only test, so
// the branch needs a test that sets supply_chain.
func TestComposeEndpointMatchesLocalComposeWithSupplyChain(t *testing.T) {
	t.Parallel()
	dossier, truth := sampleDossier()
	supplyChain := map[string]any{"inventory": []any{"pkg-a"}}
	input := serviceintel.FromServiceStory(dossier, truth)
	input.Sections = append(input.Sections,
		serviceintel.FromSupplyChainInventory(supplyChain, input.Subject, truth))
	want := serviceintel.Compose(input)
	rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose",
		mustJSON(t, ComposeRequest{Dossier: dossier, Truth: truth, SupplyChain: supplyChain, SupplyChainTruth: truth}),
		query.EnvelopeMIMEType)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	gotRaw, _ := json.Marshal(envelope.Data)
	wantRaw, _ := json.Marshal(want)
	var gotAny, wantAny any
	if err := json.Unmarshal(gotRaw, &gotAny); err != nil {
		t.Fatalf("decode got: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &wantAny); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Fatalf("endpoint compose with supply_chain differs from local:\n got: %s\nwant: %s", gotRaw, wantRaw)
	}
}

// TestComposeEndpointRejectsBadShapes pins the 413 cap, malformed-JSON,
// unknown-field, and trailing-value contracts on the compose route.
func TestComposeEndpointRejectsBadShapes(t *testing.T) {
	t.Parallel()
	dossier, truth := sampleDossier()
	good := ComposeRequest{Dossier: dossier, Truth: truth}
	t.Run("oversize", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose",
			oversizedValidJSON(maxComposeBodyBytes), query.EnvelopeMIMEType)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose",
			[]byte(`{"dossier":`), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "compose malformed json")
	})
	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose",
			withUnknownField(t, mustJSON(t, good)), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "compose unknown field")
	})
	t.Run("trailing value", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/service/report/compose",
			withTrailingValue(t, good), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "compose trailing value")
	})
}

// withoutEnvelope drops the envelope key while keeping the other capture
// inputs, pinning key presence rather than struct emptiness.
func withoutEnvelope(t *testing.T, input reportbundle.CaptureInput) []byte {
	t.Helper()
	raw := mustJSON(t, input)
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("decode capture input: %v", err)
	}
	delete(obj, "envelope")
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("re-encode capture input: %v", err)
	}
	return out
}

// TestCaptureEndpointRequiresFields pins the required surface/target/
// envelope contract: SplitTargetQuery accepts an empty target, so the
// endpoint must refuse missing fields with 400 before Capture runs.
func TestCaptureEndpointRequiresFields(t *testing.T) {
	t.Parallel()
	input := sampleCaptureInput()
	emptyEnvelope := input
	emptyEnvelope.Envelope = query.ResponseEnvelope{}
	noSurface := input
	noSurface.Surface = ""
	noTarget := input
	noTarget.Target = ""
	for name, body := range map[string][]byte{
		"empty object":     []byte(`{}`),
		"missing target":   mustJSON(t, struct{ Surface string }{Surface: "api"}),
		"empty surface":    mustJSON(t, noSurface),
		"empty target":     mustJSON(t, noTarget),
		"missing envelope": withoutEnvelope(t, input),
		"empty envelope":   mustJSON(t, emptyEnvelope),
		"nil body":         nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture", body, query.EnvelopeMIMEType)
			requireBadRequest(t, rec.Body.Bytes(), rec.Code, "capture "+name)
		})
	}
}

// TestCaptureEndpointRejectsBadShapes pins malformed-JSON, unknown-field,
// and trailing-value refusal on the capture route (the 413 cap already has
// a test).
func TestCaptureEndpointRejectsBadShapes(t *testing.T) {
	t.Parallel()
	input := sampleCaptureInput()
	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
			[]byte(`{"surface":`), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "capture malformed json")
	})
	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
			withUnknownField(t, mustJSON(t, input)), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "capture unknown field")
	})
	t.Run("trailing value", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/capture",
			withTrailingValue(t, input), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "capture trailing value")
	})
}

// TestValidateEndpointRejectsBadShapes pins the 413 cap, malformed-JSON,
// unknown-field, and trailing-value contracts on the validate route, whose
// 200-always verdict contract otherwise never exercises decodeBody's 400
// branch.
func TestValidateEndpointRejectsBadShapes(t *testing.T) {
	t.Parallel()
	bundle, err := reportbundle.Capture(sampleCaptureInput())
	if err != nil {
		t.Fatalf("local capture: %v", err)
	}
	good := ValidateRequest{Bundle: bundle}
	t.Run("oversize", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/validate",
			oversizedValidJSON(maxValidateBodyBytes), query.EnvelopeMIMEType)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/validate",
			[]byte(`{"bundle":`), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "validate malformed json")
	})
	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/validate",
			withUnknownField(t, mustJSON(t, good)), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "validate unknown field")
	})
	t.Run("trailing value", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/report/bundle/validate",
			withTrailingValue(t, good), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "validate trailing value")
	})
}

// TestScoreEndpointRejectsBadShapes pins the 413 cap, malformed-JSON,
// unknown-field, and trailing-value contracts on the score route, which
// reads raw bytes rather than sharing decodeBody.
func TestScoreEndpointRejectsBadShapes(t *testing.T) {
	t.Parallel()
	good := sampleEvidence()
	t.Run("oversize", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
			oversizedValidJSON(maxScoreBodyBytes), query.EnvelopeMIMEType)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
			[]byte(`{"version":`), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "score malformed json")
	})
	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
			withUnknownField(t, mustJSON(t, good)), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "score unknown field")
	})
	t.Run("trailing value", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
			withTrailingValue(t, good), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "score trailing value")
	})
	t.Run("missing version", func(t *testing.T) {
		t.Parallel()
		rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
			[]byte(`{}`), query.EnvelopeMIMEType)
		requireBadRequest(t, rec.Body.Bytes(), rec.Code, "score missing version")
	})
	t.Run("empty body", func(t *testing.T) {
		t.Parallel()
		for name, body := range map[string][]byte{
			"empty object": nil,
			"whitespace":   []byte("  \n "),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				rec := serveRequest(t, http.MethodPost, "/api/v0/answer-quality/score",
					body, query.EnvelopeMIMEType)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("score %s: status = %d, want 400", name, rec.Code)
				}
			})
		}
	})
}
