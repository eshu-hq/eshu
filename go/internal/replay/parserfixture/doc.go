// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parserfixture implements the deterministic-replay parser-fixture
// flavor (R-7): record and replay the facts.Envelope values the real parser and
// the Git collector's envelope-emission seam produce over a source tree,
// including their SourceRef provenance.
//
// The flavor has three pieces. Emitter (record side) runs the real
// parser.Engine over a fixture tree and, for each parseable file, builds the
// production "file" fact envelope via collector.ParserFileFactEnvelope — so a
// recording captures real envelope shape and provenance rather than a
// re-implementation. Record canonicalizes the emitted envelopes into a stable,
// reviewable fixture file (sorted keys, generation_id derived, observed_at
// collapsed, the parser payload preserved verbatim). Source (replay side) loads
// that fixture and yields the same envelopes — including provenance —
// credential-free, with no parser run and no source tree on disk, satisfying
// replay.Source so it drops into the standard collector.Service poll loop.
//
// Provenance is first-class: the fixture records each fact's SourceURI and
// (when it diverges) SourceRecordID, and the loader requires SourceURI, so a
// dropped or changed provenance field is caught by the offline round-trip gate
// rather than silently lost.
//
// Committed fixtures are portable. Parser file facts carry the file's absolute
// path in both provenance (SourceURI) and the parser-embedded payload, so a
// fixture recorded on one machine would otherwise bake in that checkout path.
// RecordOptions.RepoRoot tokenizes the repository root out on record, and
// NewSourceRehydrated / LoadFileRehydrated bind the sentinel back to the local
// checkout on replay, so a committed fixture replays byte-identically across
// machines and CI. A temp-dir recording (no RepoRoot) keeps absolute paths and
// replays through NewSource. The committed fixtures for every
// parser-backing-ledger parser live under testdata/fixtures and back the C-1
// replay-coverage manifest's parser surfaces (C-3, issue #4175); regenerate them
// with `go test ./internal/replay/parserfixture/ -update-fixtures`.
package parserfixture
