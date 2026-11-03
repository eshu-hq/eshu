// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// pythonFoldPagerLoader is a fake FactLoader that pages file facts two at a
// time into the given fold, like the production store pager. It also records
// whole-load calls, which the pager path must not make for the paged scope.
type pythonFoldPagerLoader struct {
	scopeFacts   []facts.Envelope
	fileFacts    []facts.Envelope
	valueCalls   []pythonPayloadFactCall
	foldCalls    []pythonFoldCall
	foldPageSize int
	foldErr      error
}

type pythonFoldCall struct {
	scopeID       string
	generationID  string
	payloadKey    string
	payloadValues []string
}

func (l *pythonFoldPagerLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *pythonFoldPagerLoader) ListFactsByKindAndPayloadValue(
	_ context.Context,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	l.valueCalls = append(l.valueCalls, pythonPayloadFactCall{
		scopeID: scopeID, generationID: generationID, factKind: factKind,
		payloadKey: payloadKey, payloadValues: append([]string(nil), payloadValues...),
	})
	return nil, nil
}

func (l *pythonFoldPagerLoader) FoldPythonReachabilityFiles(
	_ context.Context,
	scopeID string,
	generationID string,
	payloadKey string,
	payloadValues []string,
	fold *FileReachabilityFold,
) (int, int64, error) {
	l.foldCalls = append(l.foldCalls, pythonFoldCall{
		scopeID: scopeID, generationID: generationID, payloadKey: payloadKey,
		payloadValues: append([]string(nil), payloadValues...),
	})
	if l.foldErr != nil {
		return 0, 0, l.foldErr
	}
	page := l.foldPageSize
	if page <= 0 {
		page = 2
	}
	var files int
	var bytes int64
	for start := 0; start < len(l.fileFacts); start += page {
		end := min(start+page, len(l.fileFacts))
		for _, envelope := range l.fileFacts[start:end] {
			raw, err := json.Marshal(envelope.Payload)
			if err != nil {
				return 0, 0, err
			}
			bytes += int64(len(raw))
			if fold.Add(envelope) {
				files++
			}
		}
	}
	return files, bytes, nil
}

// pythonFoldPassEnvelopes builds the pass inputs the Python stage reads: a
// repository fact carrying the repo scope plus PyPI consumption evidence.
func pythonFoldPassEnvelopes(t *testing.T) ([]facts.Envelope, *pythonFoldPagerLoader) {
	t.Helper()
	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repository := packageSourceRepositoryFact(
		testImpactRepositoryID,
		"api",
		"https://github.com/acme/api",
		false,
		observedAt,
	)
	repository.ScopeID = "git-repository-scope:repository:r_api"
	repository.GenerationID = "git-generation-api-active"
	pass := []facts.Envelope{
		repository,
		packageConsumptionFactWithChain(
			"consume-pypi-fold",
			"pkg:pypi/requests",
			testImpactRepositoryID,
			"2.31.0",
			[]string{"requests"},
			1,
			true,
		),
	}
	loader := &pythonFoldPagerLoader{
		fileFacts: []facts.Envelope{
			pythonReachabilityFileFact(
				"file-pypi-fold-import",
				testImpactRepositoryID,
				"app.py",
				[]any{map[string]any{"name": "requests", "source": "requests", "lang": "python"}},
				[]any{map[string]any{"name": "get", "full_name": "requests.get", "line_number": 4, "lang": "python"}},
				nil,
				nil,
			),
			pythonReachabilityFileFact(
				"file-pypi-fold-decorator",
				testImpactRepositoryID,
				"views.py",
				[]any{map[string]any{"name": "flask", "source": "flask", "lang": "python"}},
				nil,
				[]any{map[string]any{"name": "view", "decorators": []any{"app.route"}}},
				[]any{map[string]any{"callee_symbol": "scip-python pypi flask 3.0.0 src/views#View.get()."}},
			),
			pythonReachabilityFileFact(
				"file-pypi-fold-tombstone",
				testImpactRepositoryID,
				"old.py",
				[]any{map[string]any{"name": "requests", "source": "requests", "lang": "python"}},
				nil,
				nil,
				nil,
			),
		},
	}
	loader.fileFacts[2].IsTombstone = true
	return pass, loader
}

// TestLoadPythonReachabilityEvidenceFactsFoldsPagedFiles is the #9860
// contract: with a paging loader the Python stage folds each page into a
// digest and retains no file envelopes, with a Python index identical to the
// same files kept as envelopes.
func TestLoadPythonReachabilityEvidenceFactsFoldsPagedFiles(t *testing.T) {
	t.Parallel()

	pass, loader := pythonFoldPassEnvelopes(t)
	handler := SupplyChainImpactHandler{FactLoader: loader}

	got, err := handler.loadPythonReachabilityEvidenceFacts(context.Background(), pass)
	if err != nil {
		t.Fatalf("loadPythonReachabilityEvidenceFacts() error = %v, want nil", err)
	}
	for _, envelope := range got.envelopes {
		if envelope.FactKind == factload.FactKindFile {
			t.Fatalf("retained file envelope %q, want files folded into a digest", envelope.FactID)
		}
	}
	if len(loader.valueCalls) != 0 {
		t.Fatalf("ListFactsByKindAndPayloadValue() calls = %d, want 0 on the pager path", len(loader.valueCalls))
	}
	if len(loader.foldCalls) != 1 {
		t.Fatalf("FoldPythonReachabilityFiles() calls = %d, want 1", len(loader.foldCalls))
	}
	call := loader.foldCalls[0]
	if call.payloadKey != "repo_id" || len(call.payloadValues) != 1 || call.payloadValues[0] != testImpactRepositoryID {
		t.Fatalf("fold call = %#v, want file facts by repo_id", call)
	}
	if got.foldedFiles != 2 {
		t.Fatalf("foldedFiles = %d, want 2 (tombstone excluded)", got.foldedFiles)
	}
	if got.foldedBytes <= 0 {
		t.Fatalf("foldedBytes = %d, want raw payload bytes read", got.foldedBytes)
	}

	wantIndex := extractPythonReachabilityEvidence(append(append([]facts.Envelope{}, pass...), loader.fileFacts...))
	gotIndex := extractPythonReachabilityEvidence(append(append([]facts.Envelope{}, pass...), got.envelopes...))
	if !reflect.DeepEqual(gotIndex, wantIndex) {
		t.Fatalf("folded Python index differs:\n got %#v\nwant %#v", gotIndex, wantIndex)
	}
}

// TestLoadPythonReachabilityEvidenceFactsKeepsBothDigests pins that the
// Python-stage digest survives alongside an active-evidence digest: the two
// digests carry distinct fact IDs so the unique-append keeps both.
func TestLoadPythonReachabilityEvidenceFactsKeepsBothDigests(t *testing.T) {
	t.Parallel()

	pass, loader := pythonFoldPassEnvelopes(t)
	activeFold := NewFileReachabilityFold(pass)
	activeFold.Add(pythonReachabilityFileFact(
		"file-active-js",
		testImpactRepositoryID,
		"app.ts",
		[]any{map[string]any{"source": "react", "name": "useState", "lang": "typescript"}},
		nil,
		nil,
		nil,
	))
	activeDigest, ok := activeFold.DigestEnvelope()
	if !ok {
		t.Fatal("DigestEnvelope() ok = false, want an active digest")
	}
	pass = append(pass, activeDigest)

	handler := SupplyChainImpactHandler{FactLoader: loader}
	got, err := handler.loadPythonReachabilityEvidenceFacts(context.Background(), pass)
	if err != nil {
		t.Fatalf("loadPythonReachabilityEvidenceFacts() error = %v, want nil", err)
	}
	merged := appendUniqueSupplyChainImpactFacts(append([]facts.Envelope{}, pass...), got.envelopes...)
	digests := 0
	for _, envelope := range merged {
		if fileReachabilityFoldFromDigest(envelope) != nil {
			digests++
		}
	}
	if digests != 2 {
		t.Fatalf("digests after unique-append = %d, want 2 (active + python)", digests)
	}

	wantIndex := extractPythonReachabilityEvidence(append(append([]facts.Envelope{}, pass...), loader.fileFacts...))
	gotIndex := extractPythonReachabilityEvidence(merged)
	if !reflect.DeepEqual(gotIndex, wantIndex) {
		t.Fatalf("two-digest Python index differs:\n got %#v\nwant %#v", gotIndex, wantIndex)
	}
}

// TestLoadPythonReachabilityEvidenceFactsFallsBackWithoutPager pins the
// fallback for loaders without paging: whole-load then fold in memory, with
// the same index and no retained file envelopes.
func TestLoadPythonReachabilityEvidenceFactsFallsBackWithoutPager(t *testing.T) {
	t.Parallel()

	pass, pager := pythonFoldPassEnvelopes(t)
	loader := &pythonReachabilityImpactLoader{
		scopeFacts:   pager.scopeFacts,
		payloadFacts: pager.fileFacts,
	}
	handler := SupplyChainImpactHandler{FactLoader: loader}

	got, err := handler.loadPythonReachabilityEvidenceFacts(context.Background(), pass)
	if err != nil {
		t.Fatalf("loadPythonReachabilityEvidenceFacts() error = %v, want nil", err)
	}
	for _, envelope := range got.envelopes {
		if envelope.FactKind == factload.FactKindFile {
			t.Fatalf("retained file envelope %q, want files folded into a digest", envelope.FactID)
		}
	}

	wantIndex := extractPythonReachabilityEvidence(append(append([]facts.Envelope{}, pass...), pager.fileFacts...))
	gotIndex := extractPythonReachabilityEvidence(append(append([]facts.Envelope{}, pass...), got.envelopes...))
	if !reflect.DeepEqual(gotIndex, wantIndex) {
		t.Fatalf("fallback Python index differs:\n got %#v\nwant %#v", gotIndex, wantIndex)
	}
}

// TestLoadPythonReachabilityEvidenceFactsMarksPagerFailuresRetryable pins the
// #9860 pager seam to the durable-queue contract: a transient pager failure
// surfaces Retryable so the scope generation retries instead of
// dead-lettering, matching the classification the fallback loader applies.
func TestLoadPythonReachabilityEvidenceFactsMarksPagerFailuresRetryable(t *testing.T) {
	t.Parallel()

	pass, loader := pythonFoldPassEnvelopes(t)
	loader.foldErr = io.ErrUnexpectedEOF
	handler := SupplyChainImpactHandler{FactLoader: loader}

	_, err := handler.loadPythonReachabilityEvidenceFacts(context.Background(), pass)
	if err == nil {
		t.Fatal("loadPythonReachabilityEvidenceFacts() error = nil, want transient pager failure")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("pager error %v does not surface Retryable, want the durable queue to retry", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("pager error %v lost its cause, want errors.Is(err, io.ErrUnexpectedEOF)", err)
	}
}
