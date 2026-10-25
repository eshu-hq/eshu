// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/epoch"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// updateContentVersionPin re-pins epoch/ledger.tsv from the current collector
// output. The epoch advances on its own when some bucket's output moved on
// unchanged input; a fixture-only change keeps it.
var updateContentVersionPin = flag.Bool("update-content-version-pin", false,
	"rewrite epoch/ledger.tsv from the current collector output")

// advanceContentEpoch forces the epoch to advance on a re-pin. Use it with
// -update-content-version-pin for a change that edits a fixture file and the
// collector or parser output for that same file together, where the ledger
// cannot tell the output change from the fixture change.
var advanceContentEpoch = flag.Bool("advance-content-epoch", false,
	"with -update-content-version-pin, advance the content epoch even when no bucket output moved on unchanged input")

// contentVersionLedgerPath is the committed ledger the epoch package embeds.
var contentVersionLedgerPath = filepath.Join("epoch", epoch.LedgerFile)

// Per-run values the digest drops: content-entity ids hash the absolute
// checkout path (the temp root differs per run), and indexed_at is the wall
// clock. The entity's name, kind, lines, and source stay in the digest.
var (
	contentVersionEntityID  = regexp.MustCompile(`e_[0-9a-f]{12}`)
	contentVersionIndexedAt = regexp.MustCompile(`"indexed_at":"[^"]*",?`)
)

// TestGenerationContentVersionPinsCollectorOutput is the gate ruling a9-117
// d6 requires (#7978), in its per-file form (#9097). The freshness hint folds
// the content epoch, and the store treats an equal hint and commit as a
// duplicate and stores nothing. This test parses a fixed multi-language fixture
// through the production snapshotter and fact builder, attributes each fact to
// the fixture file it describes, and fails when any bucket in epoch/ledger.tsv
// differs from the computed one.
//
// Fix a failure by re-pinning: `go test ./internal/collector/repo/git -run
// TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin`.
// The re-pin advances the epoch when some bucket's output moved on unchanged
// input (every repository then stores one new generation on its next sync) and
// keeps it for a fixture-only change. On a merge conflict in the ledger, take
// origin/main's copy first, then re-pin.
func TestGenerationContentVersionPinsCollectorOutput(t *testing.T) {
	computed := contentVersionFixtureBuckets(t)
	raw, err := os.ReadFile(contentVersionLedgerPath)
	if err != nil {
		t.Fatalf("read %s: %v", contentVersionLedgerPath, err)
	}
	pinned, err := epoch.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", contentVersionLedgerPath, err)
	}

	if *updateContentVersionPin {
		next, err := epoch.Repin(pinned, computed, *advanceContentEpoch)
		if err != nil {
			t.Fatalf("re-pin %s: %v", contentVersionLedgerPath, err)
		}
		if err := os.WriteFile(contentVersionLedgerPath, next.Marshal(), 0o600); err != nil {
			t.Fatalf("write %s: %v", contentVersionLedgerPath, err)
		}
		t.Logf("content epoch %s -> %s (%d buckets)", pinned.Epoch, next.Epoch, len(next.Buckets))
		return
	}
	if drift := epoch.Drift(pinned, computed); len(drift) > 0 {
		t.Fatalf("git collector output for %s does not match %s:\n  %s\n"+
			"re-pin with -update-content-version-pin (it advances the epoch when output moved on unchanged input)",
			contentVersionFixtureRoot, contentVersionLedgerPath, strings.Join(drift, "\n  "))
	}
	if !epoch.ValidEpoch(pinned.Epoch) {
		t.Fatalf("%s epoch %q is not a chained 16-hex epoch: re-pin with -update-content-version-pin -advance-content-epoch",
			contentVersionLedgerPath, pinned.Epoch)
	}
}

// TestContentVersionFixtureDigestIsDeterministic guards the gate itself: two
// runs over two different temp roots must compute the same ledger, bucket for
// bucket, or the gate would fail at random instead of on an output change.
func TestContentVersionFixtureDigestIsDeterministic(t *testing.T) {
	first := contentVersionFixtureBuckets(t)
	second := contentVersionFixtureBuckets(t)
	if len(first) != len(second) {
		t.Fatalf("bucket count not deterministic: %d then %d", len(first), len(second))
	}
	total := 0
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("bucket not deterministic: %+v then %+v", first[i], second[i])
		}
		total += first[i].FactCount
	}
	if total < 20 {
		t.Fatalf("fixture emitted %d facts; the gate needs a representative stream", total)
	}
}

// TestContentVersionFixtureCoversEveryParser keeps the content-version gate
// honest as parsers are added (review F2 on #7978): every parser the default
// registry dispatches to must parse at least one file of
// testdata/content_version, or a change to that parser could ship without a
// epoch advance and leave unchanged repositories on old facts. The list comes
// from parser.DefaultRegistry, so a new parser fails here until the fixture
// gets a file for it. Coverage is per parser key, not per extension or
// semantic family: a new extension on an existing key, or a new YAML or JSON
// dialect, passes this guard (review r2 N5). The gate still samples inputs; an
// explicit reindex is the remedy for a parser change it misses (ruling a9-125,
// #7986).
func TestContentVersionFixtureCoversEveryParser(t *testing.T) {
	t.Parallel()
	registry := parser.DefaultRegistry()
	covered := map[string]string{}
	err := filepath.WalkDir("testdata/content_version", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel("testdata/content_version", path)
		if err != nil {
			return err
		}
		if definition, ok := registry.LookupByPath(filepath.ToSlash(rel)); ok {
			if _, seen := covered[definition.ParserKey]; !seen {
				covered[definition.ParserKey] = rel
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}
	var missing []string
	for _, key := range registry.ParserKeys() {
		if _, ok := covered[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("testdata/content_version has no file for parser keys %v: add one per key, then re-pin with "+
			"-update-content-version-pin (a new file keeps the epoch; changed output for an existing file advances it)", missing)
	}
}

// TestGenerationContentVersionFoldsParserFingerprint is the #7986 closure: the
// freshness hint folds a parser-set fingerprint, so a parser registration
// change (a new key, extension, or file name) mints a new generation without a
// re-pin of the content epoch, even when the changed output is
// visible only on inputs outside testdata/content_version. The fingerprint is
// deterministic across builds, so an unchanged parser set keeps every hint.
func TestGenerationContentVersionFoldsParserFingerprint(t *testing.T) {
	t.Parallel()

	version := GenerationContentVersion()
	want := "parsers=" + parser.DefaultRegistry().Fingerprint()
	if !strings.Contains(version, want) {
		t.Fatalf("GenerationContentVersion %q does not fold the parser fingerprint %q: "+
			"a parser-set change would keep old facts as unchanged", version, want)
	}
	if again := GenerationContentVersion(); again != version {
		t.Fatalf("GenerationContentVersion not stable: %q then %q", version, again)
	}
}

// contentVersionFixtureFacts copies testdata/content_version to a temp root,
// snapshots it with the production parser engine, builds its fact stream, and
// returns one normalized line per fact (temp root, content-entity ids, and
// indexed_at removed) with the bucket it is attributed to.
func contentVersionFixtureFacts(t *testing.T, inputs map[string]string) []contentVersionFact {
	t.Helper()
	// Resolve the temp root first: on macOS t.TempDir() is under /var, a
	// symlink to /private/var, and the snapshot would then emit file graph ids
	// such as "../../<root>/x" where Linux emits "x", so the pinned digest
	// would depend on the host (it failed CI's Linux race shard on #7997).
	tempRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	root := filepath.Join(tempRoot, "content-version-repo")
	copyContentVersionFixture(t, contentVersionFixtureRoot, root)
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-content-version",
		time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = root
	}
	roots := []string{resolvedRoot, root}
	facts := make([]contentVersionFact, 0, len(envelopes))
	for _, envelope := range envelopes {
		encoded, err := json.Marshal(map[string]any{
			"fact_kind":      envelope.FactKind,
			"stable_key":     envelope.StableFactKey,
			"schema_version": envelope.SchemaVersion,
			"tombstone":      envelope.IsTombstone,
			"payload":        envelope.Payload,
		})
		if err != nil {
			t.Fatalf("encode fact %s: %v", envelope.FactKind, err)
		}
		line := strings.ReplaceAll(string(encoded), resolvedRoot, "<root>")
		line = strings.ReplaceAll(line, root, "<root>")
		line = contentVersionEntityID.ReplaceAllString(line, "e_<id>")
		bucket, missed := contentVersionBucket(envelope.Payload, roots, inputs)
		facts = append(facts, contentVersionFact{
			Kind:   envelope.FactKind,
			Bucket: bucket,
			Line:   contentVersionIndexedAt.ReplaceAllString(line, ""),
			Missed: missed,
		})
	}
	return facts
}

func copyContentVersionFixture(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o750)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, body, 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}
