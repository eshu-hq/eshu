package confluence

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type fakeClient struct {
	space            Space
	spacePages       []Page
	treePageIDs      []string
	pagesByID        map[string]Page
	forbiddenPageIDs map[string]struct{}
}

func (f *fakeClient) GetSpace(_ context.Context, _ string) (Space, error) {
	return f.space, nil
}

func (f *fakeClient) ListSpacePages(_ context.Context, _ string, _ int) ([]Page, error) {
	return f.spacePages, nil
}

func (f *fakeClient) ListPageTree(_ context.Context, _ string, _ int) ([]string, error) {
	return f.treePageIDs, nil
}

func (f *fakeClient) GetPage(_ context.Context, id string) (Page, error) {
	if _, ok := f.forbiddenPageIDs[id]; ok {
		return Page{}, ErrPermissionDenied
	}
	page, ok := f.pagesByID[id]
	if !ok {
		var latest Page
		for _, candidate := range f.spacePages {
			if candidate.ID == id && candidate.Version.Number >= latest.Version.Number {
				latest = candidate
			}
		}
		if latest.ID != "" {
			return latest, nil
		}
		return Page{}, errors.New("missing test page")
	}
	return page, nil
}

func drainFacts(t *testing.T, stream <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var envelopes []facts.Envelope
	for envelope := range stream {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func factsByKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	var matched []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matched = append(matched, envelope)
		}
	}
	return matched
}

func assertFactCount(t *testing.T, envelopes []facts.Envelope, kind string, want int) {
	t.Helper()
	if got := len(factsByKind(envelopes, kind)); got != want {
		t.Fatalf("%s count = %d, want %d", kind, got, want)
	}
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return value
}

func payloadStrings(payload map[string]any, key string) []string {
	values, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if ok {
			out = append(out, text)
		}
	}
	return out
}

func payloadInt(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case string:
		var out int
		_, _ = fmt.Sscanf(value, "%d", &out)
		return out
	default:
		return 0
	}
}

func payloadMap(payload map[string]any, key string) map[string]any {
	value, ok := payload[key].(map[string]any)
	if !ok {
		return nil
	}
	return value
}
