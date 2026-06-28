// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeEndpoint stands in for the real provider during recording. It records the
// Authorization header it saw so a test can assert the recorder reached it with
// a live credential that the tape must not retain.
type fakeEndpoint struct {
	mu       sync.Mutex
	seenAuth []string
	handler  http.HandlerFunc
}

func (f *fakeEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seenAuth = append(f.seenAuth, r.Header.Get("Authorization"))
	f.mu.Unlock()
	f.handler(w, r)
}

func newFakeServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *fakeEndpoint) {
	t.Helper()
	fake := &fakeEndpoint{handler: handler}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return server, fake
}

func TestRecordThenReplayRoundTrip(t *testing.T) {
	server, fake := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":["b","a"]}`)
	})

	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/loki/api/v1/labels?end=2&start=1", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer super-secret-token")

	resp, err := recClient.Do(req)
	if err != nil {
		t.Fatalf("record round trip: %v", err)
	}
	gotBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(gotBody), "success") {
		t.Fatalf("recorder starved the caller body: %q", gotBody)
	}

	// The recorder must have reached the live endpoint with the credential.
	if len(fake.seenAuth) != 1 || fake.seenAuth[0] != "Bearer super-secret-token" {
		t.Fatalf("recorder did not forward live credential: %v", fake.seenAuth)
	}

	tape := recorder.Tape("loki")
	if len(tape.Interactions) != 1 {
		t.Fatalf("want 1 interaction, got %d", len(tape.Interactions))
	}
	// The tape must be credential-free.
	canonical, err := MarshalTape(tape)
	if err != nil {
		t.Fatalf("marshal tape: %v", err)
	}
	if bytes.Contains(canonical, []byte("super-secret-token")) {
		t.Fatalf("tape leaked credential:\n%s", canonical)
	}
	if !bytes.Contains(canonical, []byte(redactedMarker)) {
		t.Fatalf("tape did not record the redaction sentinel:\n%s", canonical)
	}

	// Replay with NO server running (close it first to prove no network).
	server.Close()

	replayer, err := NewReplayer(tape, Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	replayClient := &http.Client{Transport: replayer}
	// A replay request carries no credential; it must still match the recorded
	// (redacted) key.
	rreq, _ := http.NewRequest(http.MethodGet, server.URL+"/loki/api/v1/labels?start=1&end=2", nil)
	rresp, err := replayClient.Do(rreq)
	if err != nil {
		t.Fatalf("replay round trip: %v", err)
	}
	rBody, _ := io.ReadAll(rresp.Body)
	_ = rresp.Body.Close()
	if rresp.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d", rresp.StatusCode)
	}
	if !strings.Contains(string(rBody), "success") {
		t.Fatalf("replay body mismatch: %q", rBody)
	}
}

func TestReplayUnmatchedRequestErrorsLoudly(t *testing.T) {
	server, _ := newFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/recorded", nil)
	if _, err := recClient.Do(req); err != nil {
		t.Fatalf("record: %v", err)
	}
	tape := recorder.Tape("test")
	server.Close()

	replayer, err := NewReplayer(tape, Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}

	// A request that was never recorded must error and make no network call.
	// Use a transport-level call so the http.Client retry/redirect layer does
	// not mask the error.
	unreq, _ := http.NewRequest(http.MethodGet, "http://203.0.113.1:9/never-recorded", nil)
	resp, err := replayer.RoundTrip(unreq)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("want error for unmatched request, got response %v", resp)
	}
	if !errors.Is(err, ErrUnmatchedRequest) {
		t.Fatalf("want ErrUnmatchedRequest, got %v", err)
	}
}

func TestReplayMatchesPostBodyKeyOrderIndependent(t *testing.T) {
	server, _ := newFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"echo":"ok"}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}
	body := strings.NewReader(`{"b":2,"a":1}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/query", body)
	req.Header.Set("Content-Type", "application/json")
	if _, err := recClient.Do(req); err != nil {
		t.Fatalf("record: %v", err)
	}
	tape := recorder.Tape("test")
	server.Close()

	replayer, _ := NewReplayer(tape, Config{})
	replayClient := &http.Client{Transport: replayer}
	// Same body, different key order — must still match because JSON bodies are
	// canonicalized before the key is computed.
	rreq, _ := http.NewRequest(http.MethodPost, server.URL+"/query", strings.NewReader(`{"a":1,"b":2}`))
	rreq.Header.Set("Content-Type", "application/json")
	resp, err := replayClient.Do(rreq)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// A different body must NOT match.
	dreq, _ := http.NewRequest(http.MethodPost, server.URL+"/query", strings.NewReader(`{"a":99}`))
	dreq.Header.Set("Content-Type", "application/json")
	if _, err := replayer.RoundTrip(dreq); !errors.Is(err, ErrUnmatchedRequest) {
		t.Fatalf("different body should be unmatched, got %v", err)
	}
}

func TestSecretQueryParamRedactedAndStillMatches(t *testing.T) {
	server, _ := newFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/data?token=secret123&page=1", nil)
	if _, err := recClient.Do(req); err != nil {
		t.Fatalf("record: %v", err)
	}
	tape := recorder.Tape("test")
	canonical, _ := MarshalTape(tape)
	if bytes.Contains(canonical, []byte("secret123")) {
		t.Fatalf("query secret leaked:\n%s", canonical)
	}
	server.Close()

	replayer, _ := NewReplayer(tape, Config{})
	replayClient := &http.Client{Transport: replayer}
	// Replay with a different token value: redaction makes the key independent
	// of the secret value, so it must still match.
	rreq, _ := http.NewRequest(http.MethodGet, server.URL+"/data?page=1&token=DIFFERENT", nil)
	resp, err := replayClient.Do(rreq)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestVolatileQueryParamMatchesAcrossRuns(t *testing.T) {
	server, _ := newFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	cfg := Config{VolatileQueryParams: []string{"end"}}
	recorder := New(cfg)
	recClient := &http.Client{Transport: recorder}
	// Record with one timestamp value.
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/series?end=1700000000&match=x", nil)
	if _, err := recClient.Do(req); err != nil {
		t.Fatalf("record: %v", err)
	}
	tape := recorder.Tape("test")

	// The recorded request keeps the real (non-secret) volatile value for review.
	canonical, _ := MarshalTape(tape)
	if !bytes.Contains(canonical, []byte("1700000000")) {
		t.Fatalf("volatile value should be recorded for review:\n%s", canonical)
	}
	server.Close()

	replayer, _ := NewReplayer(tape, cfg)
	replayClient := &http.Client{Transport: replayer}
	// Replay with a DIFFERENT timestamp value: must still match.
	rreq, _ := http.NewRequest(http.MethodGet, server.URL+"/series?end=1799999999&match=x", nil)
	resp, err := replayClient.Do(rreq)
	if err != nil {
		t.Fatalf("replay with different volatile value: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// But a non-volatile param change must NOT match.
	dreq, _ := http.NewRequest(http.MethodGet, server.URL+"/series?end=1799999999&match=DIFFERENT", nil)
	if _, err := replayer.RoundTrip(dreq); !errors.Is(err, ErrUnmatchedRequest) {
		t.Fatalf("non-volatile change should be unmatched, got %v", err)
	}
}

func TestEmptyTapeReplayerRejected(t *testing.T) {
	_, err := NewReplayer(Tape{SchemaVersion: CurrentSchemaVersion}, Config{})
	if !errors.Is(err, errEmptyTape) {
		t.Fatalf("want errEmptyTape, got %v", err)
	}
}

func TestParseTapeRejectsBadSchema(t *testing.T) {
	_, err := ParseTape([]byte(`{"schema_version":"99","interactions":[]}`))
	if err == nil {
		t.Fatalf("want error for bad schema version")
	}
}

func TestRepeatedQueryValuesAreUnambiguous(t *testing.T) {
	// ?match=a,b&match=c and ?match=a&match=b,c carry distinct value groupings
	// and MUST hash to distinct request keys, or one recorded interaction would
	// overwrite the other and replay could serve the wrong response. PromQL and
	// LogQL match[] selectors contain commas, so this is a real collision.
	server, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"q":"`+r.URL.RawQuery+`"}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}

	for _, raw := range []string{"match=a,b&match=c", "match=a&match=b,c"} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/series?"+raw, nil)
		if _, err := recClient.Do(req); err != nil {
			t.Fatalf("record %q: %v", raw, err)
		}
	}
	tape := recorder.Tape("test")
	if len(tape.Interactions) != 2 {
		t.Fatalf("want 2 distinct interactions, got %d (key collision)", len(tape.Interactions))
	}
	server.Close()

	// A genuine reordering of the SAME value grouping must still match, so
	// order-independence within a key is preserved.
	replayer, _ := NewReplayer(tape, Config{})
	replayClient := &http.Client{Transport: replayer}
	rreq, _ := http.NewRequest(http.MethodGet, server.URL+"/series?match=c&match=a,b", nil)
	resp, err := replayClient.Do(rreq)
	if err != nil {
		t.Fatalf("reordered same values should match: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestParseTapeRejectsTrailingJSON(t *testing.T) {
	valid := `{"schema_version":"1","interactions":[{"request_key":"k",` +
		`"request":{"method":"GET","path":"/"},"response":{"status_code":200}}]}`
	// A second top-level JSON value appended after the document must be rejected.
	_, err := ParseTape([]byte(valid + "\n{}"))
	if err == nil {
		t.Fatalf("want error for trailing JSON, got nil")
	}
	if !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("want trailing-data error, got %v", err)
	}
	// The clean document must still parse.
	if _, err := ParseTape([]byte(valid)); err != nil {
		t.Fatalf("clean tape should parse: %v", err)
	}
}

func TestConcurrentRecordIsRaceFree(t *testing.T) {
	server, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"path":"`+r.URL.Path+`"}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/p/"+string(rune('a'+i%26)), nil)
			resp, err := recClient.Do(req)
			if err != nil {
				t.Errorf("concurrent record: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}(i)
	}
	wg.Wait()

	tape := recorder.Tape("test")
	if len(tape.Interactions) == 0 {
		t.Fatalf("no interactions recorded")
	}
	// MarshalTape must be deterministic regardless of recording goroutine order.
	a, _ := MarshalTape(tape)
	b, _ := MarshalTape(tape)
	if !bytes.Equal(a, b) {
		t.Fatalf("MarshalTape not deterministic")
	}
}

// redactedMarker is the sentinel the canonical core uses for redacted values.
const redactedMarker = "<redacted>"
