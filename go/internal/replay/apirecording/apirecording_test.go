// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apirecording_test

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/replay/apirecording"
)

// updateGolden rewrites the recorded API-shape golden from the live handler
// output. Run `go test ./internal/replay/apirecording -update-golden` after an
// intentional, reviewed shape change.
var updateGolden = flag.Bool("update-golden", false, "rewrite the apirecording golden file from live handler output")

const goldenPath = "testdata/query-playbooks.recording.json"

// queryPlaybookHandler builds the real query mux mounting only the deterministic
// query-playbook handler. The handler reads in-process catalog data and never
// touches Postgres, a graph backend, or an LLM, so it is the canonical offline
// success+refusal subject for the recording gate. The same mux is the dispatch
// target MCP tool calls resolve through (R-9), so a recording taken here is
// reusable across transports.
func queryPlaybookHandler() http.Handler {
	mux := http.NewServeMux()
	router := &query.APIRouter{
		Playbooks: &query.QueryPlaybookHandler{Profile: query.ProfileProduction},
	}
	router.Mount(mux)
	return mux
}

// recordingRequests is the representative request set the golden asserts on: a
// GET success (catalog list), a POST success (resolver), and a POST refusal
// (unknown playbook → bounded not_found error envelope). Each opts into the
// canonical envelope so the recorded shape is the envelope, not a backward-compat
// payload.
func recordingRequests() []apirecording.RequestDescriptor {
	envelope := map[string]string{"Accept": query.EnvelopeMIMEType}
	jsonEnvelope := map[string]string{
		"Accept":       query.EnvelopeMIMEType,
		"Content-Type": "application/json",
	}
	return []apirecording.RequestDescriptor{
		{
			Name:      "list-catalog-success",
			Transport: apirecording.TransportHTTP,
			Method:    http.MethodGet,
			Path:      "/api/v0/query-playbooks",
			Headers:   envelope,
		},
		{
			Name:      "resolve-success",
			Transport: apirecording.TransportHTTP,
			Method:    http.MethodPost,
			Path:      "/api/v0/query-playbooks/resolve",
			Headers:   jsonEnvelope,
			Body:      `{"playbook_id":"service_story_citation","inputs":{"service_name":"payments-api","environment":"prod"}}`,
		},
		{
			Name:      "resolve-unknown-refusal",
			Transport: apirecording.TransportHTTP,
			Method:    http.MethodPost,
			Path:      "/api/v0/query-playbooks/resolve",
			Headers:   jsonEnvelope,
			Body:      `{"playbook_id":"missing","inputs":{}}`,
		},
	}
}

// TestQueryPlaybookRecordingMatchesGolden is the offline shape gate: it records
// the live handler output and asserts it against the committed golden. Under
// -update-golden it rewrites the golden instead. A query-handler or envelope
// shape change that is not reflected in the golden fails this test offline.
func TestQueryPlaybookRecordingMatchesGolden(t *testing.T) {
	handler := queryPlaybookHandler()
	opts := apirecording.DefaultOptions()

	recording, err := apirecording.Record(handler, recordingRequests(), opts)
	if err != nil {
		t.Fatalf("Record() error = %v, want nil", err)
	}

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := apirecording.WriteFile(goldenPath, recording); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		t.Logf("rewrote golden %s", goldenPath)
		return
	}

	golden, err := apirecording.LoadFile(goldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v; run with -update-golden to create it", goldenPath, err)
	}

	if err := apirecording.Assert(handler, golden, opts); err != nil {
		t.Fatalf("recorded API shape diverged from golden:\n%v", err)
	}

	// Cover the success and refusal branches explicitly so a golden that drops an
	// exchange is caught, not silently passed.
	gotNames := exchangeNames(recording)
	for _, want := range []string{"list-catalog-success", "resolve-success", "resolve-unknown-refusal"} {
		if _, ok := gotNames[want]; !ok {
			t.Fatalf("recording missing exchange %q; got %v", want, gotNames)
		}
	}
}

// TestAssertCatchesDeliberateShapeChange proves the gate is not false-green: a
// recording whose committed refusal status is mutated to a different shape MUST
// fail Assert against the live handler. If this test ever passes silently, the
// gate is asserting nothing.
func TestAssertCatchesDeliberateShapeChange(t *testing.T) {
	handler := queryPlaybookHandler()
	opts := apirecording.DefaultOptions()

	golden, err := apirecording.LoadFile(goldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", goldenPath, err)
	}

	// First confirm the unmutated golden matches, so the failure below is
	// attributable to the mutation and not a pre-existing drift.
	if err := apirecording.Assert(handler, golden, opts); err != nil {
		t.Fatalf("baseline golden must match live handler before mutation: %v", err)
	}

	mutated := mutateRefusalStatus(t, golden, "resolve-unknown-refusal")
	err = apirecording.Assert(handler, mutated, opts)
	if err == nil {
		t.Fatal("Assert() = nil on a mutated golden; the gate is false-green")
	}
}

// TestAssertCatchesDeliberateBodyChange proves a body-level shape change (an
// extra recorded field that the live handler does not emit) is also caught, not
// only a status change.
func TestAssertCatchesDeliberateBodyChange(t *testing.T) {
	handler := queryPlaybookHandler()
	opts := apirecording.DefaultOptions()

	golden, err := apirecording.LoadFile(goldenPath)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", goldenPath, err)
	}

	mutated := mutateInjectBodyField(t, golden, "list-catalog-success")
	if err := apirecording.Assert(handler, mutated, opts); err == nil {
		t.Fatal("Assert() = nil after injecting a phantom body field; the gate is false-green")
	}
}

// TestRecordingRoundTripsThroughDisk proves Marshal/LoadFile is lossless: a
// recording written and read back asserts clean against the live handler.
func TestRecordingRoundTripsThroughDisk(t *testing.T) {
	handler := queryPlaybookHandler()
	opts := apirecording.DefaultOptions()

	recording, err := apirecording.Record(handler, recordingRequests(), opts)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "roundtrip.recording.json")
	if err := apirecording.WriteFile(path, recording); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	reloaded, err := apirecording.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if err := apirecording.Assert(handler, reloaded, opts); err != nil {
		t.Fatalf("round-tripped recording diverged: %v", err)
	}
}

// TestRecordingIsDeterministic proves two records of the same handler produce
// byte-identical golden bytes — the canonical core collapses the per-request
// volatile fields so a re-record does not churn.
func TestRecordingIsDeterministic(t *testing.T) {
	handler := queryPlaybookHandler()
	opts := apirecording.DefaultOptions()

	first, err := apirecording.Record(handler, recordingRequests(), opts)
	if err != nil {
		t.Fatalf("Record() first error = %v", err)
	}
	second, err := apirecording.Record(handler, recordingRequests(), opts)
	if err != nil {
		t.Fatalf("Record() second error = %v", err)
	}
	firstBytes, err := apirecording.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal(first) error = %v", err)
	}
	secondBytes, err := apirecording.Marshal(second)
	if err != nil {
		t.Fatalf("Marshal(second) error = %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("re-record is not byte-identical:\nfirst=\n%s\nsecond=\n%s", firstBytes, secondBytes)
	}
}

// exchangeNames returns the set of request names in a recording.
func exchangeNames(r apirecording.Recording) map[string]struct{} {
	names := make(map[string]struct{}, len(r.Exchanges))
	for _, ex := range r.Exchanges {
		names[ex.Request.Name] = struct{}{}
	}
	return names
}

// mutateRefusalStatus returns a deep copy of r with the named exchange's
// recorded status changed, so Assert must report a status divergence.
func mutateRefusalStatus(t *testing.T, r apirecording.Recording, name string) apirecording.Recording {
	t.Helper()
	out := deepCopy(t, r)
	for i := range out.Exchanges {
		if out.Exchanges[i].Request.Name == name {
			out.Exchanges[i].Response.Status = http.StatusTeapot
			return out
		}
	}
	t.Fatalf("exchange %q not found to mutate", name)
	return out
}

// mutateInjectBodyField returns a deep copy of r with a phantom top-level field
// injected into the named exchange's recorded body, so Assert must report a body
// divergence against the live handler that never emits it.
func mutateInjectBodyField(t *testing.T, r apirecording.Recording, name string) apirecording.Recording {
	t.Helper()
	out := deepCopy(t, r)
	for i := range out.Exchanges {
		if out.Exchanges[i].Request.Name != name {
			continue
		}
		body, ok := out.Exchanges[i].Response.Body.(map[string]any)
		if !ok {
			t.Fatalf("exchange %q body is %T, want map for injection", name, out.Exchanges[i].Response.Body)
		}
		body["__phantom_field__"] = "not-emitted-by-handler"
		out.Exchanges[i].Response.Body = body
		return out
	}
	t.Fatalf("exchange %q not found to mutate", name)
	return out
}

// deepCopy round-trips a recording through JSON to produce an independent copy
// so a mutation does not alias the original.
func deepCopy(t *testing.T, r apirecording.Recording) apirecording.Recording {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal for deep copy: %v", err)
	}
	var out apirecording.Recording
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal for deep copy: %v", err)
	}
	return out
}
