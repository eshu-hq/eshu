// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// noteOverCapPinFiles notes count pin files of bodyBytes each at distinct
// depths and returns the candidate map and the paths.
func noteOverCapPinFiles(count, bodyBytes int) (map[string]Candidate, []string) {
	candidates := map[string]Candidate{}
	body := strings.Repeat("x", bodyBytes)
	paths := make([]string, count)
	for i := range paths {
		paths[i] = strings.Repeat("d/", i) + ".nvmrc"
		NoteRuntimeDeclarationCandidate(candidates, paths[i], body)
	}
	return candidates, paths
}

// TestNoteRuntimeDeclarationCandidateRetainsNoBodyOverTheByteCap proves the
// stream does not carry an over-cap pin body until emission: 50 pin files of
// 1 MiB each, at different depths, leave the candidate state holding no body
// larger than the byte cap, and each still records its full size.
func TestNoteRuntimeDeclarationCandidateRetainsNoBodyOverTheByteCap(t *testing.T) {
	t.Parallel()

	const files, bodyBytes = 50, 1 << 20
	candidates, paths := noteOverCapPinFiles(files, bodyBytes)
	if len(candidates) != files {
		t.Fatalf("candidates = %d, want %d", len(candidates), files)
	}
	retained := 0
	for _, path := range paths {
		candidate := candidates[path]
		retained += len(candidate.Body)
		if len(candidate.Body) > runtimedeclaration.MaxBytesPerFile {
			t.Errorf("%s retains a %d byte body over the %d byte cap", path, len(candidate.Body), runtimedeclaration.MaxBytesPerFile)
		}
		if candidate.ByteSize != bodyBytes {
			t.Errorf("%s ByteSize = %d, want %d", path, candidate.ByteSize, bodyBytes)
		}
	}
	if retained != 0 {
		t.Fatalf("retained %d body bytes for %d over-cap files, want 0", retained, files)
	}
}

// TestNoteRuntimeDeclarationCandidateKeepsABodyAtTheCap proves a body exactly
// at the cap is retained whole so emission can parse it.
func TestNoteRuntimeDeclarationCandidateKeepsABodyAtTheCap(t *testing.T) {
	t.Parallel()

	candidates := map[string]Candidate{}
	body := strings.Repeat("a", runtimedeclaration.MaxBytesPerFile)
	NoteRuntimeDeclarationCandidate(candidates, "p/.nvmrc", body)
	got := candidates["p/.nvmrc"]
	if got.Body != body || got.ByteSize != len(body) {
		t.Fatalf("at-cap candidate = len(Body)=%d ByteSize=%d, want the whole %d byte body", len(got.Body), got.ByteSize, len(body))
	}
}

// TestEmitRuntimeDeclarationFactsReportsEveryOverCapFileWithoutFacts proves an
// over-cap candidate, recorded by size only, emits no fact and reaches onSkip
// with the byte reason and its original size, while an in-cap pin file beside
// it still emits.
func TestEmitRuntimeDeclarationFactsReportsEveryOverCapFileWithoutFacts(t *testing.T) {
	t.Parallel()

	const files, bodyBytes = 5, 1 << 20
	candidates, paths := noteOverCapPinFiles(files, bodyBytes)
	NoteRuntimeDeclarationCandidate(candidates, "ok/.nvmrc", "20\n")

	ch := make(chan facts.Envelope, 16)
	writer := model.NewFactStreamWriter(ch, nil, "retention-test")
	skipped := map[string]runtimedeclaration.Skip{}
	EmitRuntimeDeclarationFactsForCandidates(writer, "repo", "scope", "gen",
		time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC), candidates,
		func(path string, skip runtimedeclaration.Skip) { skipped[path] = skip })

	close(ch)
	emitted := 0
	for range ch {
		emitted++
	}
	if emitted != 1 {
		t.Fatalf("envelopes = %d, want 1 (the in-cap file only)", emitted)
	}
	if len(skipped) != files {
		t.Fatalf("skipped = %d, want %d", len(skipped), files)
	}
	for _, path := range paths {
		skip := skipped[path]
		if skip.Reason != runtimedeclaration.SkipReasonByteCap || skip.ByteSize != bodyBytes || skip.TokenCount != 0 {
			t.Errorf("skip[%s] = %+v, want byte cap, ByteSize=%d, no token count", path, skip, bodyBytes)
		}
	}
}
