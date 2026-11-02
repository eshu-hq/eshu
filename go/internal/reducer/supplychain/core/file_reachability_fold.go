// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// FileReachabilityDigestFactKind is the in-memory kind of the one envelope a
// FileReachabilityFold stands in for. It never reaches Postgres or a writer:
// the pass builds it after the active-evidence load and the index builders
// consume it in place of the parser file envelopes it replaced.
const FileReachabilityDigestFactKind = "reducer_file_reachability_digest"

const fileReachabilityDigestPayloadKey = "fold"

// FileReachabilityFold reduces parser `file` envelopes to the JS/TS package
// reachability index and the Python reachability evidence they would have
// produced, then lets the caller drop each decoded payload (#9781).
//
// A pass's npm batch can name most of the fleet's JS/TS files. Keeping those
// decoded payloads until the index builders ran held about 140 KB of heap per
// file, and eight concurrent passes exceeded the container limit. Only the
// package roots, the evidence priority, and the fact ID of the winning file
// survive the reduction, so the fold's size follows the number of distinct
// (repository, package) pairs, not the payload bytes.
//
// The fold runs the same per-file bodies as buildJSTSPackageReachabilityIndex
// and extractPythonReachabilityEvidence (jsTSPackageReachabilityIndex.addFile
// and addPythonReachabilityFile), so a folded file and a retained envelope
// read the same keys the same way. Order matters because the first usage of
// the best evidence priority wins and Python evidence lists keep fact order:
// Add files in the order the pass would have appended them. A fold is owned by
// one pass and one goroutine; it has no locks.
type FileReachabilityFold struct {
	seen   map[string]struct{}
	jsTS   jsTSPackageReachabilityIndex
	python map[string]pythonReachabilityRepositoryEvidence
	files  int
}

// NewFileReachabilityFold starts a fold for a pass that already holds
// existing. Their file fact IDs are remembered, so a file the pass already has
// is not folded a second time, as appendUniqueSupplyChainImpactFacts would not
// append it twice.
func NewFileReachabilityFold(existing []facts.Envelope) *FileReachabilityFold {
	fold := &FileReachabilityFold{
		seen:   map[string]struct{}{},
		jsTS:   jsTSPackageReachabilityIndex{repositories: map[string]*jsTSRepositoryReachability{}},
		python: map[string]pythonReachabilityRepositoryEvidence{},
	}
	for _, envelope := range existing {
		if prior := fileReachabilityFoldFromDigest(envelope); prior != nil {
			for factID := range prior.seen {
				fold.seen[factID] = struct{}{}
			}
			continue
		}
		if envelope.FactKind == factload.FactKindFile && envelope.FactID != "" {
			fold.seen[envelope.FactID] = struct{}{}
		}
	}
	return fold
}

// Add folds one parser file envelope and reports whether it was new. A
// non-file kind or a tombstone is not folded, and a fact ID the fold or the
// pass already holds is not folded again. A file with an empty fact ID is
// always folded, matching appendUniqueSupplyChainImpactFacts.
func (f *FileReachabilityFold) Add(envelope facts.Envelope) bool {
	if envelope.FactKind != factload.FactKindFile {
		return false
	}
	if envelope.FactID != "" {
		if _, duplicate := f.seen[envelope.FactID]; duplicate {
			return false
		}
		f.seen[envelope.FactID] = struct{}{}
	}
	if envelope.IsTombstone {
		return false
	}
	f.files++
	f.jsTS.addFile(envelope)
	addPythonReachabilityFile(f.python, envelope)
	return true
}

// Files is how many file envelopes the fold absorbed.
func (f *FileReachabilityFold) Files() int {
	if f == nil {
		return 0
	}
	return f.files
}

// DigestEnvelope returns the envelope the index builders read in place of the
// folded files, or false when nothing was folded. Append it where the folded
// files would have gone: after the pass's earlier files and before the files
// a later stage loads.
func (f *FileReachabilityFold) DigestEnvelope() (facts.Envelope, bool) {
	if f == nil || f.files == 0 {
		return facts.Envelope{}, false
	}
	return facts.Envelope{
		FactID:   FileReachabilityDigestFactKind,
		FactKind: FileReachabilityDigestFactKind,
		Payload:  map[string]any{fileReachabilityDigestPayloadKey: f},
	}, true
}

// fileReachabilityFoldFromDigest returns the fold a digest envelope carries,
// or nil for any other envelope.
func fileReachabilityFoldFromDigest(envelope facts.Envelope) *FileReachabilityFold {
	if envelope.FactKind != FileReachabilityDigestFactKind {
		return nil
	}
	fold, _ := envelope.Payload[fileReachabilityDigestPayloadKey].(*FileReachabilityFold)
	return fold
}

// AppendEvidenceFactIDs appends the fact IDs an envelope stands for in the
// pass's evidence to dst. A plain envelope is its own FactID. A digest stands
// for every parser file fact the fold absorbed, so it contributes those file
// IDs, in byte order, and not its synthetic ID: a finding's reachability
// evidence cites the files, and the protected writer accepts an evidence fact
// ID only when the loader listed it (#9855). Tombstoned files are listed too;
// they were part of the pass's evidence before the fold.
func AppendEvidenceFactIDs(dst []string, envelope facts.Envelope) []string {
	fold := fileReachabilityFoldFromDigest(envelope)
	if fold == nil {
		return append(dst, envelope.FactID)
	}
	ids := make([]string, 0, len(fold.seen))
	for factID := range fold.seen {
		ids = append(ids, factID)
	}
	slices.Sort(ids)
	return append(dst, ids...)
}

// The Python reachability reader's per-file body and merge live here, beside
// the fold that reuses them, so both read file facts the same way (#9781).

func extractPythonReachabilityEvidence(envelopes []facts.Envelope) map[string]pythonReachabilityRepositoryEvidence {
	out := make(map[string]pythonReachabilityRepositoryEvidence)
	for _, envelope := range envelopes {
		if fold := fileReachabilityFoldFromDigest(envelope); fold != nil {
			mergePythonReachabilityEvidence(out, fold.python)
			continue
		}
		addPythonReachabilityFile(out, envelope)
	}
	return out
}

// addPythonReachabilityFile folds one parser file envelope into out. It is the
// one body extractPythonReachabilityEvidence and FileReachabilityFold.Add run.
func addPythonReachabilityFile(out map[string]pythonReachabilityRepositoryEvidence, envelope facts.Envelope) {
	if envelope.FactKind != factload.FactKindFile || envelope.IsTombstone {
		return
	}
	repositoryID := payloadcore.PayloadStr(envelope.Payload, "repo_id")
	if repositoryID == "" {
		return
	}
	fileData := payloadcore.PayloadMap(envelope.Payload, "parsed_file_data")
	if fileData == nil {
		return
	}
	evidence := out[repositoryID]
	evidence.ensureMaps()
	aliases := collectPythonImportEvidence(&evidence, envelope.FactID, fileData)
	collectPythonCallEvidence(&evidence, envelope.FactID, fileData, aliases)
	collectPythonDecoratorEvidence(&evidence, envelope.FactID, fileData, aliases)
	for _, edge := range payloadcore.MapSlice(fileData["function_calls_scip"]) {
		if symbol := strings.TrimSpace(payloadcore.PayloadStr(edge, "callee_symbol")); symbol != "" {
			evidence.scipSymbols[symbol] = append(evidence.scipSymbols[symbol], envelope.FactID)
		}
	}
	out[repositoryID] = evidence
}

// mergePythonReachabilityEvidence appends later's evidence after out's, the
// order the same files would have produced one after the other.
func mergePythonReachabilityEvidence(
	out map[string]pythonReachabilityRepositoryEvidence,
	later map[string]pythonReachabilityRepositoryEvidence,
) {
	for repositoryID, source := range later {
		evidence := out[repositoryID]
		evidence.ensureMaps()
		for root, ids := range source.importedModules {
			evidence.importedModules[root] = append(evidence.importedModules[root], ids...)
		}
		for root, ids := range source.calledModules {
			evidence.calledModules[root] = append(evidence.calledModules[root], ids...)
		}
		for root, ids := range source.decoratedModules {
			evidence.decoratedModules[root] = append(evidence.decoratedModules[root], ids...)
		}
		for symbol, ids := range source.scipSymbols {
			evidence.scipSymbols[symbol] = append(evidence.scipSymbols[symbol], ids...)
		}
		evidence.dynamicImport = evidence.dynamicImport || source.dynamicImport
		evidence.pluginLoading = evidence.pluginLoading || source.pluginLoading
		out[repositoryID] = evidence
	}
}
