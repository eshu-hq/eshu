// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import (
	"encoding/json"
	"errors"
	"testing"
)

// stubFetcher records the path it was asked for and replays a canned response.
type stubFetcher struct {
	path     string
	response ServiceEnvelope
	err      error
}

func (s *stubFetcher) GetEnvelope(path string, result any) error {
	s.path = path
	if s.err != nil {
		return s.err
	}
	encoded, err := json.Marshal(s.response)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

// TestFetchServiceStoryBuildsCanonicalPath pins the request URL: the selector is
// path-escaped so a name with a slash or a space survives, and every selector
// the operator passed becomes a query parameter.
func TestFetchServiceStoryBuildsCanonicalPath(t *testing.T) {
	t.Parallel()

	client := &stubFetcher{response: ServiceEnvelope{Data: map[string]any{"service_name": "api"}}}
	envelope, err := FetchServiceStory(client, "team/api service", ServiceQuery{
		Repo:        "github.com/eshu-hq/eshu",
		Environment: "prod",
		ServiceID:   "svc-1",
	})
	if err != nil {
		t.Fatalf("FetchServiceStory returned %v, want nil", err)
	}
	want := "/api/v0/services/team%2Fapi%20service/story?environment=prod&repo=github.com%2Feshu-hq%2Feshu&service_id=svc-1"
	if client.path != want {
		t.Fatalf("request path =\n%q\nwant\n%q", client.path, want)
	}
	if envelope.Data["service_name"] != "api" {
		t.Fatalf("decoded envelope = %#v, want the stub response", envelope.Data)
	}
}

// TestFetchServiceStoryOmitsEmptySelectors pins that an unset flag contributes
// no query parameter at all. A blank parameter would reach the API as an empty
// filter, and it would show up in the transport error text an operator reads.
func TestFetchServiceStoryOmitsEmptySelectors(t *testing.T) {
	t.Parallel()

	client := &stubFetcher{}
	if _, err := FetchServiceStory(client, "api", ServiceQuery{}); err != nil {
		t.Fatalf("FetchServiceStory returned %v, want nil", err)
	}
	if want := "/api/v0/services/api/story"; client.path != want {
		t.Fatalf("request path = %q, want %q with no query string", client.path, want)
	}
}

// TestFetchServiceStoryReturnsTransportErrorUnwrapped pins that the transport
// error travels back untouched. go/cmd/eshu renders its text verbatim and
// classifies the failure by matching substrings of it, so a wrap here would
// change both the operator's message and the exit code.
func TestFetchServiceStoryReturnsTransportErrorUnwrapped(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("request failed: connection refused")
	client := &stubFetcher{err: sentinel}
	envelope, err := FetchServiceStory(client, "api", ServiceQuery{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("FetchServiceStory error = %v, want the transport error itself", err)
	}
	if err.Error() != sentinel.Error() {
		t.Fatalf("error text = %q, want %q unchanged", err.Error(), sentinel.Error())
	}
	if envelope.Data != nil || envelope.Error != nil {
		t.Fatalf("envelope = %#v, want the zero value on a transport failure", envelope)
	}
}

// TestServiceFreshnessState covers the state go/cmd/eshu exits 4 on and the
// absent-truth case that must read as empty rather than panic.
func TestServiceFreshnessState(t *testing.T) {
	t.Parallel()

	stale := ServiceEnvelope{Truth: map[string]any{"freshness": map[string]any{"state": "stale"}}}
	if got := ServiceFreshnessState(stale); got != "stale" {
		t.Fatalf("ServiceFreshnessState = %q, want %q", got, "stale")
	}
	if got := ServiceFreshnessState(ServiceEnvelope{}); got != "" {
		t.Fatalf("ServiceFreshnessState of an empty envelope = %q, want empty", got)
	}
}

// TestServiceStatus covers the status go/cmd/eshu exits 5 on and the absent-data
// case.
func TestServiceStatus(t *testing.T) {
	t.Parallel()

	partial := ServiceEnvelope{Data: map[string]any{"code_to_runtime_trace": map[string]any{"status": "partial"}}}
	if got := ServiceStatus(partial); got != "partial" {
		t.Fatalf("ServiceStatus = %q, want %q", got, "partial")
	}
	if got := ServiceStatus(ServiceEnvelope{}); got != "" {
		t.Fatalf("ServiceStatus of an empty envelope = %q, want empty", got)
	}
}
