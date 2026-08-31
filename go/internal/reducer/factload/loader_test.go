// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// retryable reports whether err self-classifies as retryable through the
// interface the Postgres queue reads via errors.As.
func retryable(err error) bool {
	var r interface{ Retryable() bool }
	return errors.As(err, &r) && r.Retryable()
}

// TestClassifyFactLoadErrorMarksOnlyTransportFailuresRetryable pins the single
// highest-consequence decision in this package. Widening it turns a permanent
// error into an infinite retry; narrowing it dead-letters a whole scope
// generation on a transient outage.
func TestClassifyFactLoadErrorMarksOnlyTransportFailuresRetryable(t *testing.T) {
	t.Parallel()

	if got := ClassifyFactLoadError(nil); got != nil {
		t.Errorf("nil error classified as %v, want nil", got)
	}

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "sentinel unexpected EOF", err: io.ErrUnexpectedEOF, want: true},
		{name: "wrapped sentinel", err: fmt.Errorf("reading facts: %w", io.ErrUnexpectedEOF), want: true},
		{name: "message match, mixed case", err: errors.New("driver: Unexpected EOF on stream"), want: true},
		{name: "permanent decode failure", err: errors.New("column missing"), want: false},
		{name: "context cancelled", err: context.Canceled, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyFactLoadError(tc.err)
			if retryable(got) != tc.want {
				t.Errorf("retryable = %v, want %v (err %v)", retryable(got), tc.want, tc.err)
			}
			if !errors.Is(got, tc.err) {
				t.Errorf("classification lost the cause: errors.Is(%v, %v) = false", got, tc.err)
			}
		})
	}
}

// TestRetryableFactLoadErrorReportsItsFailureClass pins the durable failure
// class an operator reads off a dead-lettered work item.
func TestRetryableFactLoadErrorReportsItsFailureClass(t *testing.T) {
	t.Parallel()

	err := ClassifyFactLoadError(io.ErrUnexpectedEOF)
	var fc interface{ FailureClass() string }
	if !errors.As(err, &fc) {
		t.Fatal("classified error does not expose FailureClass")
	}
	if got := fc.FailureClass(); got != "fact_load_transient" {
		t.Errorf("FailureClass = %q, want %q", got, "fact_load_transient")
	}
}

// kindLoader implements the optional kind-filtering extension.
type kindLoader struct {
	byKind []facts.Envelope
	all    []facts.Envelope
	sawAll bool
}

func (k *kindLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	k.sawAll = true
	return k.all, nil
}

func (k *kindLoader) ListFactsByKind(context.Context, string, string, []string) ([]facts.Envelope, error) {
	return k.byKind, nil
}

// plainLoader implements only the base port.
type plainLoader struct {
	all    []facts.Envelope
	sawAll bool
}

func (p *plainLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	p.sawAll = true
	return p.all, nil
}

// TestLoadFactsForKindsPushesDownWhenItCan pins that a store implementing the
// kind extension is asked to filter, and is not asked for the whole generation.
func TestLoadFactsForKindsPushesDownWhenItCan(t *testing.T) {
	t.Parallel()

	l := &kindLoader{
		byKind: []facts.Envelope{{FactID: "filtered"}},
		all:    []facts.Envelope{{FactID: "a"}, {FactID: "b"}},
	}
	got, err := LoadFactsForKinds(context.Background(), l, "scope", "gen", []string{"repository"})
	if err != nil {
		t.Fatalf("LoadFactsForKinds returned %v", err)
	}
	if l.sawAll {
		t.Error("push-down store was still asked for the whole generation")
	}
	if len(got) != 1 || got[0].FactID != "filtered" {
		t.Errorf("got %v, want the store-filtered result", got)
	}
}

// TestLoadFactsForKindsReturnsTheWholeGenerationWhenItCannot pins the fallback
// as it actually behaves: it returns the FULL scope generation UNFILTERED. This
// package applies no in-process filter — the calling domain handler does. The
// package docs claimed otherwise once, and an agent believing that claim could
// drop the extension and silently hand every handler the whole generation.
func TestLoadFactsForKindsReturnsTheWholeGenerationWhenItCannot(t *testing.T) {
	t.Parallel()

	l := &plainLoader{all: []facts.Envelope{
		{FactID: "a", FactKind: "repository"},
		{FactID: "b", FactKind: "file"},
	}}
	got, err := LoadFactsForKinds(context.Background(), l, "scope", "gen", []string{"repository"})
	if err != nil {
		t.Fatalf("LoadFactsForKinds returned %v", err)
	}
	if !l.sawAll {
		t.Error("fallback did not use the full FactLoader contract")
	}
	if len(got) != 2 {
		t.Fatalf("got %d envelopes, want 2 — the fallback must NOT filter in process", len(got))
	}
}

// payloadValueLoader implements the optional payload-value-filtering
// extension and records exactly what LoadFactsForKindAndPayloadValue passed
// it, so tests can pin that the prologue's cleaning (trim, drop-empty,
// dedup) happened before the push-down call rather than trusting the caller
// already passed a clean slice. It also implements the base FactLoader port
// so a test can detect a fallback call it should not have made.
type payloadValueLoader struct {
	result []facts.Envelope

	called        bool
	gotFactKind   string
	gotPayloadKey string
	gotValues     []string
}

func (p *payloadValueLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	p.called = true
	return nil, nil
}

func (p *payloadValueLoader) ListFactsByKindAndPayloadValue(
	_ context.Context,
	_ string,
	_ string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	p.called = true
	p.gotFactKind = factKind
	p.gotPayloadKey = payloadKey
	p.gotValues = payloadValues
	return p.result, nil
}

// TestLoadFactsForKindAndPayloadValueCleansValuesBeforePushDown pins that the
// prologue's payloadcore.CleanFactFilterValues call runs before the push-down
// query, using input shaped like the real caller
// (supply_chain_impact_python_reachability.go), which hands the loader a
// runtime-derived repo-ID slice that is not pre-cleaned.
func TestLoadFactsForKindAndPayloadValueCleansValuesBeforePushDown(t *testing.T) {
	t.Parallel()

	l := &payloadValueLoader{result: []facts.Envelope{{FactID: "hit"}}}
	got, err := LoadFactsForKindAndPayloadValue(
		context.Background(), l, "scope", "gen",
		"  repository  ", "  repo_id  ",
		[]string{" repo-a ", "repo-a", "", "  ", "repo-b"},
	)
	if err != nil {
		t.Fatalf("LoadFactsForKindAndPayloadValue returned %v", err)
	}
	if !l.called {
		t.Fatal("push-down store was never asked")
	}
	if l.gotFactKind != "repository" {
		t.Errorf("factKind = %q, want trimmed %q", l.gotFactKind, "repository")
	}
	if l.gotPayloadKey != "repo_id" {
		t.Errorf("payloadKey = %q, want trimmed %q", l.gotPayloadKey, "repo_id")
	}
	wantValues := []string{"repo-a", "repo-b"}
	if len(l.gotValues) != len(wantValues) {
		t.Fatalf("payloadValues = %v, want %v (whitespace-trimmed, empty-dropped, deduped)", l.gotValues, wantValues)
	}
	for i, want := range wantValues {
		if l.gotValues[i] != want {
			t.Errorf("payloadValues[%d] = %q, want %q", i, l.gotValues[i], want)
		}
	}
	if len(got) != 1 || got[0].FactID != "hit" {
		t.Errorf("got %v, want the store-filtered result", got)
	}
}

// TestLoadFactsForKindAndPayloadValueGuardsBlankInputs pins that each of the
// three guard conditions — blank fact kind, blank payload key, and a
// payload-values slice that cleans to empty — short-circuits before the
// loader is ever consulted, on either the push-down or the fallback path. A
// neutered guard would let a blank query reach the store instead.
func TestLoadFactsForKindAndPayloadValueGuardsBlankInputs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		factKind      string
		payloadKey    string
		payloadValues []string
	}{
		{name: "blank fact kind", factKind: "   ", payloadKey: "repo_id", payloadValues: []string{"repo-a"}},
		{name: "blank payload key", factKind: "repository", payloadKey: "  ", payloadValues: []string{"repo-a"}},
		{name: "payload values clean to empty", factKind: "repository", payloadKey: "repo_id", payloadValues: []string{"", "   "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := &payloadValueLoader{result: []facts.Envelope{{FactID: "should-not-be-seen"}}}
			got, err := LoadFactsForKindAndPayloadValue(
				context.Background(), l, "scope", "gen",
				tc.factKind, tc.payloadKey, tc.payloadValues,
			)
			if err != nil {
				t.Fatalf("LoadFactsForKindAndPayloadValue returned %v", err)
			}
			if got != nil {
				t.Errorf("got %v, want nil — the guard must short-circuit", got)
			}
			if l.called {
				t.Error("loader was consulted despite a blank guard input")
			}
		})
	}
}
