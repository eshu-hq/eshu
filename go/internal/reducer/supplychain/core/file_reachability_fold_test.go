// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// foldCorpus builds a deterministic mix of parser file facts that exercises
// every key the JS/TS and Python readers consume: import aliases, calls that
// match and miss them, SCIP edges, language-less rows, decorators, other
// languages, tombstones, repository ID fallbacks, and shared packages across
// files so the first-wins tie-break matters.
func foldCorpus(seed int64, n int) []facts.Envelope {
	rng := rand.New(rand.NewSource(seed))
	packages := []string{"react", "@scope/api", "lodash", "api", "@scope/other", "node:fs", "./local"}
	languages := []string{"typescript", "javascript", "tsx", "jsx", "python", "go", ""}
	out := make([]facts.Envelope, 0, n)
	for i := 0; i < n; i++ {
		var imports, calls, scip, functions, classes []any
		for k := 0; k < rng.Intn(4); k++ {
			pkg := packages[rng.Intn(len(packages))]
			row := map[string]any{"source": pkg, "name": fmt.Sprintf("n%d", k), "alias": fmt.Sprintf("a%d", k), "import_type": "import"}
			if rng.Intn(5) == 0 {
				row["import_type"] = "reexport"
			}
			if rng.Intn(3) == 0 {
				row["lang"] = "python"
			}
			imports = append(imports, row)
		}
		for k := 0; k < rng.Intn(6); k++ {
			row := map[string]any{"full_name": fmt.Sprintf("a%d.run", rng.Intn(4)), "name": "run"}
			if rng.Intn(4) == 0 {
				row["full_name"] = "importlib.import_module"
			}
			if rng.Intn(4) == 0 {
				row["lang"] = "typescript"
			}
			calls = append(calls, row)
		}
		for k := 0; k < rng.Intn(3); k++ {
			scip = append(scip, map[string]any{
				"package":       "npm " + packages[rng.Intn(len(packages))] + " 1.0.0",
				"callee_symbol": "scip-typescript npm " + packages[rng.Intn(len(packages))] + " 1.0.0 x/y().",
			})
		}
		for k := 0; k < rng.Intn(3); k++ {
			functions = append(functions, map[string]any{"name": "f", "decorators": []any{"a0.route", "plain"}, "source": "const x = 1"})
		}
		classes = append(classes, map[string]any{"name": "C", "decorators": []any{"a1.component"}})
		fileData := map[string]any{
			"language": languages[rng.Intn(len(languages))],
			"imports":  imports, "function_calls": calls, "function_calls_scip": scip,
			"functions": functions, "classes": classes,
			"variables": []any{map[string]any{"name": "ignored"}},
		}
		payload := map[string]any{
			"repo_id": fmt.Sprintf("repo-%d", rng.Intn(3)), "relative_path": fmt.Sprintf("src/f%d.ts", i),
			"parsed_file_data": fileData,
		}
		switch rng.Intn(8) {
		case 0:
			payload["repository_id"] = payload["repo_id"]
			delete(payload, "repo_id")
		case 1:
			delete(payload, "repo_id")
		case 2:
			delete(payload, "parsed_file_data")
		}
		out = append(out, facts.Envelope{
			FactID: fmt.Sprintf("file-%04d", i), FactKind: factload.FactKindFile,
			ScopeID: fmt.Sprintf("repo-%d", rng.Intn(3)), IsTombstone: rng.Intn(15) == 0, Payload: payload,
		})
	}
	return out
}

func foldReachabilityIndexes(envelopes []facts.Envelope) (jsTSPackageReachabilityIndex, map[string]pythonReachabilityRepositoryEvidence) {
	return buildJSTSPackageReachabilityIndex(envelopes), extractPythonReachabilityEvidence(envelopes)
}

// TestFileReachabilityFoldMatchesEnvelopeBuilders is the #9781 parity
// property: file facts read before, through, and after a fold produce the same
// JS/TS and Python reachability indexes as the same facts kept as envelopes in
// the same order. The layout mirrors the pass: base files, then the folded
// active-evidence files, then Python-loaded files.
func TestFileReachabilityFoldMatchesEnvelopeBuilders(t *testing.T) {
	t.Parallel()

	for seed := int64(1); seed <= 60; seed++ {
		all := foldCorpus(seed, 90)
		base, folded, post := all[:20], all[20:70], all[70:]

		wantJSTS, wantPython := foldReachabilityIndexes(all)

		fold := NewFileReachabilityFold(base)
		for _, envelope := range folded {
			fold.Add(envelope)
		}
		digest, ok := fold.DigestEnvelope()
		if !ok {
			t.Fatalf("seed %d: DigestEnvelope() ok = false, want a digest for %d folded files", seed, len(folded))
		}
		layout := append(append(append([]facts.Envelope{}, base...), digest), post...)
		gotJSTS, gotPython := foldReachabilityIndexes(layout)

		if !reflect.DeepEqual(gotJSTS, wantJSTS) {
			t.Fatalf("seed %d: JS/TS index differs:\n got %#v\nwant %#v", seed, gotJSTS, wantJSTS)
		}
		if !reflect.DeepEqual(gotPython, wantPython) {
			t.Fatalf("seed %d: Python evidence differs:\n got %#v\nwant %#v", seed, gotPython, wantPython)
		}
	}
}

// TestFileReachabilityFoldDedupesLikeAppendUnique pins that a file already in
// the pass (base load) or folded earlier (an earlier round) is not folded
// again, and that the later Python load cannot re-add a folded file.
func TestFileReachabilityFoldDedupesLikeAppendUnique(t *testing.T) {
	t.Parallel()

	all := foldCorpus(7, 30)
	base := all[:5]
	fold := NewFileReachabilityFold(base)
	if fold.Add(base[0]) {
		t.Fatal("Add(base file) = true, want false: the pass already holds it")
	}
	if !fold.Add(all[10]) && !all[10].IsTombstone {
		t.Fatal("Add(new file) = false, want true")
	}
	if fold.Add(all[10]) {
		t.Fatal("Add(same file twice) = true, want false")
	}
	digest, ok := fold.DigestEnvelope()
	if !ok {
		t.Fatal("DigestEnvelope() ok = false, want a digest")
	}
	withDigest := appendUniqueSupplyChainImpactFacts(append([]facts.Envelope{}, base...), digest)
	merged := appendUniqueSupplyChainImpactFacts(withDigest, all[10])
	for _, envelope := range merged {
		if envelope.FactID == all[10].FactID {
			t.Fatalf("appendUnique re-added folded file %q, want it dropped", envelope.FactID)
		}
	}
}

// TestFileReachabilityFoldIgnoresNonFileAndTombstoneEnvelopes pins the filters
// the builders apply before reading a payload.
func TestFileReachabilityFoldIgnoresNonFileAndTombstoneEnvelopes(t *testing.T) {
	t.Parallel()

	fold := NewFileReachabilityFold(nil)
	if fold.Add(facts.Envelope{FactID: "x", FactKind: "vulnerability.cve"}) {
		t.Fatal("Add(non-file) = true, want false")
	}
	if fold.Add(facts.Envelope{FactID: "t", FactKind: factload.FactKindFile, IsTombstone: true}) {
		t.Fatal("Add(tombstone) = true, want false")
	}
	if _, ok := fold.DigestEnvelope(); ok {
		t.Fatal("DigestEnvelope() ok = true for an empty fold, want false")
	}
}

// TestAppendEvidenceFactIDsExpandsADigestInByteOrder pins the helper both
// authorized-loader loops use (#9855): a plain envelope contributes its own ID,
// and a digest contributes the folded file IDs, including tombstoned ones the
// fold saw, in byte order and without its synthetic ID.
func TestAppendEvidenceFactIDsExpandsADigestInByteOrder(t *testing.T) {
	t.Parallel()

	fold := NewFileReachabilityFold([]facts.Envelope{{FactID: "file-existing", FactKind: factload.FactKindFile}})
	for _, id := range []string{"file-b", "file-Z", "file-a"} {
		fold.Add(facts.Envelope{FactID: id, FactKind: factload.FactKindFile, Payload: map[string]any{"repo_id": "r"}})
	}
	fold.Add(facts.Envelope{FactID: "file-tomb", FactKind: factload.FactKindFile, IsTombstone: true})
	digest, ok := fold.DigestEnvelope()
	if !ok {
		t.Fatal("DigestEnvelope() ok = false, want a digest")
	}

	got := AppendEvidenceFactIDs([]string{"before"}, digest)
	want := []string{"before", "file-Z", "file-a", "file-b", "file-existing", "file-tomb"}
	if !slices.Equal(got, want) {
		t.Fatalf("AppendEvidenceFactIDs(digest) = %v, want %v (byte order, no digest ID)", got, want)
	}
	plain := AppendEvidenceFactIDs(nil, facts.Envelope{FactID: "scope-fact", FactKind: "package_registry.package"})
	if !slices.Equal(plain, []string{"scope-fact"}) {
		t.Fatalf("AppendEvidenceFactIDs(plain) = %v, want [scope-fact]", plain)
	}
}
