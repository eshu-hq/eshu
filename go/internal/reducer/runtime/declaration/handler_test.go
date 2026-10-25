// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type stubFactLoader struct {
	envelopes []facts.Envelope
	err       error
}

func (f *stubFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return f.envelopes, f.err
}

// recordingWriter is a Writer fake that records every call in order, so a test
// can assert retract-before-write and the exact retract scope.
type recordingWriter struct {
	mu             sync.Mutex
	order          []string
	writeRows      []map[string]any
	writeGen       string
	retractRepoIDs []string
	retractPaths   map[string][]string
	writeErr       error
}

func (w *recordingWriter) WriteRuntimeDeclarations(_ context.Context, rows []map[string]any, generationID, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.order = append(w.order, "write")
	w.writeRows = append(w.writeRows, rows...)
	w.writeGen = generationID
	return w.writeErr
}

func (w *recordingWriter) RetractRuntimeDeclarations(_ context.Context, repoIDs []string, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.order = append(w.order, "retract")
	w.retractRepoIDs = append(w.retractRepoIDs, repoIDs...)
	return nil
}

func (w *recordingWriter) RetractRuntimeDeclarationsByPath(_ context.Context, pathsByRepoID map[string][]string, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.order = append(w.order, "retract_by_path")
	if w.retractPaths == nil {
		w.retractPaths = map[string][]string{}
	}
	for repoID, paths := range pathsByRepoID {
		w.retractPaths[repoID] = append(w.retractPaths[repoID], paths...)
	}
	return nil
}

func intPtr(v int) *int { return &v }

func versionEnvelope(repoID, relativePath string, line, order int, runtime, raw string, major *int) facts.Envelope {
	payload, err := factschema.EncodeRuntimeDeclarationVersion(runtimedeclarationv1.Version{
		RepoID:       repoID,
		RelativePath: relativePath,
		ScopeDir:     ".",
		LineNumber:   line,
		OrderIndex:   order,
		SourceKind:   "tool_versions",
		Runtime:      runtime,
		RawSpec:      raw,
		SpecKind:     runtimedeclarationv1.SpecKindExact,
		VersionMajor: major,
	})
	if err != nil {
		panic(err)
	}
	return facts.Envelope{
		FactKind:      facts.RuntimeDeclarationVersionFactKind,
		SchemaVersion: facts.RuntimeDeclarationSchemaVersionV1,
		Payload:       payload,
	}
}

func repositoryEnvelope(repoID string, delta bool, changed, deleted []string) facts.Envelope {
	payload := map[string]any{"repo_id": repoID, "graph_id": repoID}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = changed
		payload["delta_deleted_relative_paths"] = deleted
	}
	return facts.Envelope{FactKind: "repository", Payload: payload}
}

func intent(attempt int) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-2",
		Domain:       reducercontract.DomainRuntimeDeclarationProjection,
		AttemptCount: attempt,
	}
}

func hasPrior(context.Context, string, string) (bool, error) { return true, nil }

func TestHandleRejectsWrongDomainAndMissingDeps(t *testing.T) {
	t.Parallel()

	h := Handler{FactLoader: &stubFactLoader{}, Writer: &recordingWriter{}}
	bad := intent(1)
	bad.Domain = reducercontract.DomainCodeownersOwnership
	if _, err := h.Handle(context.Background(), bad); err == nil {
		t.Fatal("Handle(wrong domain) = nil error, want error")
	}
	if _, err := (Handler{Writer: &recordingWriter{}}).Handle(context.Background(), intent(1)); err == nil {
		t.Fatal("Handle(nil FactLoader) = nil error, want error")
	}
	if _, err := (Handler{FactLoader: &stubFactLoader{}}).Handle(context.Background(), intent(1)); err == nil {
		t.Fatal("Handle(nil Writer) = nil error, want error")
	}
}

// TestHandleFullGenerationRetractsWholeRepositoryThenWrites covers the
// positive case: a full generation retracts every declaration the repository
// owned, then writes one row per fact keyed on the five-part identity.
func TestHandleFullGenerationRetractsWholeRepositoryThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	h := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", false, nil, nil),
			versionEnvelope("repository:r1", ".tool-versions", 1, 0, "node", "20.11.1", intPtr(20)),
			versionEnvelope("repository:r1", ".tool-versions", 1, 1, "node", "18.19.0", intPtr(18)),
		}},
		Writer:               writer,
		PriorGenerationCheck: hasPrior,
	}
	result, err := h.Handle(context.Background(), intent(1))
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !reflect.DeepEqual(writer.order, []string{"retract", "write"}) {
		t.Fatalf("call order = %v, want [retract write]", writer.order)
	}
	if !reflect.DeepEqual(writer.retractRepoIDs, []string{"repository:r1"}) {
		t.Fatalf("retract repos = %v", writer.retractRepoIDs)
	}
	if len(writer.writeRows) != 2 || result.CanonicalWrites != 2 {
		t.Fatalf("wrote %d rows, CanonicalWrites=%d; want 2 and 2", len(writer.writeRows), result.CanonicalWrites)
	}
	if writer.writeGen != "gen-2" {
		t.Errorf("write generation = %q, want gen-2", writer.writeGen)
	}
	row := writer.writeRows[1]
	for key, want := range map[string]any{
		"repo_id": "repository:r1", "relative_path": ".tool-versions", "line_number": 1,
		"runtime": "node", "order_index": 1, "raw_spec": "18.19.0", "spec_kind": "exact", "version_major": 18,
	} {
		if got := row[key]; !reflect.DeepEqual(got, want) {
			t.Errorf("row[%q] = %#v, want %#v", key, got, want)
		}
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Errorf("Status = %q, want succeeded", result.Status)
	}
}

// TestHandleNullVersionPartsStayNull proves an alias or range is written with
// nil version parts, never zero-filled: a 0 major would read as "below 20".
func TestHandleNullVersionPartsStayNull(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	h := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", false, nil, nil),
			versionEnvelope("repository:r1", ".nvmrc", 1, 0, "node", "lts/iron", nil),
		}},
		Writer: writer,
	}
	if _, err := h.Handle(context.Background(), intent(1)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if got, ok := writer.writeRows[0]["version_major"]; !ok || got != nil {
		t.Fatalf("version_major = %#v (present=%v), want present and nil", got, ok)
	}
}

// TestHandleFullGenerationWithNoPinFilesStillRetracts is the "file deleted"
// case on a full generation: no runtime facts survive, but the repository fact
// names the repository, so its stale declarations are still retracted.
func TestHandleFullGenerationWithNoPinFilesStillRetracts(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	h := Handler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{repositoryEnvelope("repository:r1", false, nil, nil)}},
		Writer:               writer,
		PriorGenerationCheck: hasPrior,
	}
	if _, err := h.Handle(context.Background(), intent(1)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !reflect.DeepEqual(writer.order, []string{"retract"}) || !reflect.DeepEqual(writer.retractRepoIDs, []string{"repository:r1"}) {
		t.Fatalf("order=%v repos=%v, want one whole-repo retract of repository:r1", writer.order, writer.retractRepoIDs)
	}
}

// TestHandleDeltaGenerationRetractsOnlyChangedAndDeletedPaths is the negative
// case for over-retraction: a delta generation carries facts only for the pin
// files it changed, so a whole-repo retract would erase every unchanged pin
// file's declarations. The retract is scoped to changed plus deleted paths.
func TestHandleDeltaGenerationRetractsOnlyChangedAndDeletedPaths(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	h := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", true, []string{"svc/.nvmrc", "main.go"}, []string{".python-version"}),
			versionEnvelope("repository:r1", "svc/.nvmrc", 1, 0, "node", "20", intPtr(20)),
		}},
		Writer:               writer,
		PriorGenerationCheck: hasPrior,
	}
	if _, err := h.Handle(context.Background(), intent(1)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !reflect.DeepEqual(writer.order, []string{"retract_by_path", "write"}) {
		t.Fatalf("call order = %v, want [retract_by_path write]", writer.order)
	}
	if len(writer.retractRepoIDs) != 0 {
		t.Fatalf("whole-repo retract on a delta generation: %v", writer.retractRepoIDs)
	}
	got := append([]string(nil), writer.retractPaths["repository:r1"]...)
	sort.Strings(got)
	if want := []string{".python-version", "main.go", "svc/.nvmrc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retract paths = %v, want %v", got, want)
	}
}

func TestHandleSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	h := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", false, nil, nil),
			versionEnvelope("repository:r1", ".nvmrc", 1, 0, "node", "20", intPtr(20)),
		}},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := h.Handle(context.Background(), intent(1)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !reflect.DeepEqual(writer.order, []string{"write"}) {
		t.Fatalf("order = %v, want [write] (no prior generation, first attempt)", writer.order)
	}
	// A retry must retract even without a prior generation: the first attempt
	// may have written before it failed.
	retryWriter := &recordingWriter{}
	h.Writer = retryWriter
	if _, err := h.Handle(context.Background(), intent(2)); err != nil {
		t.Fatalf("Handle(retry) error = %v", err)
	}
	if !reflect.DeepEqual(retryWriter.order, []string{"retract", "write"}) {
		t.Fatalf("retry order = %v, want [retract write]", retryWriter.order)
	}
}

// TestHandleQuarantinesMissingRequiredFieldWithoutZeroFill proves a fact with
// no runtime dead-letters as input_invalid and is never written with an empty
// identity, while its valid sibling still projects.
func TestHandleQuarantinesMissingRequiredFieldWithoutZeroFill(t *testing.T) {
	t.Parallel()

	bad := versionEnvelope("repository:r1", ".nvmrc", 1, 0, "node", "20", intPtr(20))
	delete(bad.Payload, "runtime")
	writer := &recordingWriter{}
	h := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", false, nil, nil),
			bad,
			versionEnvelope("repository:r1", ".python-version", 1, 0, "python", "3.12.1", intPtr(3)),
		}},
		Writer: writer,
	}
	result, err := h.Handle(context.Background(), intent(1))
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.writeRows) != 1 || writer.writeRows[0]["runtime"] != "python" {
		t.Fatalf("written rows = %#v, want only the python row", writer.writeRows)
	}
	if result.SubSignals["input_invalid_facts"] != 1 {
		t.Fatalf("SubSignals = %#v, want input_invalid_facts=1", result.SubSignals)
	}
}

// TestHandleDeduplicatesRepeatedIdentity proves a replayed fact with the same
// five-part key projects one row, so counts stay stable across replays.
func TestHandleDeduplicatesRepeatedIdentity(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	h := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", false, nil, nil),
			versionEnvelope("repository:r1", ".nvmrc", 1, 0, "node", "18", intPtr(18)),
			versionEnvelope("repository:r1", ".nvmrc", 1, 0, "node", "20", intPtr(20)),
		}},
		Writer: writer,
	}
	if _, err := h.Handle(context.Background(), intent(1)); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.writeRows) != 1 || writer.writeRows[0]["raw_spec"] != "20" {
		t.Fatalf("rows = %#v, want one row carrying the last fact (20)", writer.writeRows)
	}
}

func TestHandlePropagatesLoaderAndWriterErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	h := Handler{FactLoader: &stubFactLoader{err: boom}, Writer: &recordingWriter{}}
	if _, err := h.Handle(context.Background(), intent(1)); !errors.Is(err, boom) {
		t.Fatalf("loader error = %v, want wrapping %v", err, boom)
	}
	h = Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			repositoryEnvelope("repository:r1", false, nil, nil),
			versionEnvelope("repository:r1", ".nvmrc", 1, 0, "node", "20", intPtr(20)),
		}},
		Writer: &recordingWriter{writeErr: boom},
	}
	if _, err := h.Handle(context.Background(), intent(1)); !errors.Is(err, boom) {
		t.Fatalf("writer error = %v, want wrapping %v", err, boom)
	}
}
