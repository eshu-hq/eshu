// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// updateContentVersionPin re-pins testdata/content_version_pin.json. Run it
// only in the same change that bumps gitGenerationContentVersion.
var updateContentVersionPin = flag.Bool("update-content-version-pin", false,
	"rewrite testdata/content_version_pin.json from the current collector output")

// contentVersionPin binds the collector output contract version to a digest of
// the fact stream the git collector emits for testdata/content_version.
type contentVersionPin struct {
	ContentVersion string `json:"content_version"`
	FactDigest     string `json:"fact_digest"`
	FactCount      int    `json:"fact_count"`
}

const contentVersionPinPath = "testdata/content_version_pin.json"

// Per-run values the digest drops: content-entity ids hash the absolute
// checkout path (the temp root differs per run), and indexed_at is the wall
// clock. The entity's name, kind, lines, and source stay in the digest.
var (
	contentVersionEntityID  = regexp.MustCompile(`e_[0-9a-f]{12}`)
	contentVersionIndexedAt = regexp.MustCompile(`"indexed_at":"[^"]*",?`)
)

// TestGenerationContentVersionPinsCollectorOutput is the gate ruling a9-117
// d6 requires (#7978). The freshness hint folds gitGenerationContentVersion,
// and the store treats an equal hint and commit as a duplicate and stores
// nothing. A parser or collector change that alters emitted facts without a
// version bump would therefore keep stale facts as "unchanged". This test
// parses a fixed multi-language fixture through the production snapshotter and
// fact builder, and fails when the normalized fact digest moves while the
// version constant does not.
//
// To accept a deliberate output change: bump gitGenerationContentVersion, then
// run `go test ./internal/collector/repo/git -run
// TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin`.
// The bump makes every repository store one new generation on its next sync.
func TestGenerationContentVersionPinsCollectorOutput(t *testing.T) {
	digest, count := contentVersionFixtureDigest(t)
	got := contentVersionPin{ContentVersion: gitGenerationContentVersion, FactDigest: digest, FactCount: count}

	if *updateContentVersionPin {
		encoded, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatalf("encode pin: %v", err)
		}
		if err := os.WriteFile(contentVersionPinPath, append(encoded, '\n'), 0o600); err != nil {
			t.Fatalf("write pin: %v", err)
		}
		return
	}
	raw, err := os.ReadFile(contentVersionPinPath)
	if err != nil {
		t.Fatalf("read %s: %v", contentVersionPinPath, err)
	}
	var want contentVersionPin
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode %s: %v", contentVersionPinPath, err)
	}
	switch {
	case want.ContentVersion == got.ContentVersion && want.FactDigest != got.FactDigest:
		t.Fatalf("git collector output for testdata/content_version changed (%d facts, digest %s; pinned %d facts, %s) "+
			"but gitGenerationContentVersion is still %q: bump it so unchanged repositories store the new output, "+
			"then re-pin with -update-content-version-pin", got.FactCount, got.FactDigest, want.FactCount, want.FactDigest, got.ContentVersion)
	case want != got:
		t.Fatalf("content version pin = %+v, want %+v: re-pin with -update-content-version-pin", want, got)
	}
}

// TestContentVersionFixtureDigestIsDeterministic guards the gate itself: two
// runs over two different temp roots must agree, or the gate would fail at
// random instead of on an output change.
func TestContentVersionFixtureDigestIsDeterministic(t *testing.T) {
	first, firstCount := contentVersionFixtureDigest(t)
	second, secondCount := contentVersionFixtureDigest(t)
	if first != second || firstCount != secondCount {
		t.Fatalf("fixture digest not deterministic: %s/%d then %s/%d", first, firstCount, second, secondCount)
	}
	if firstCount < 20 {
		t.Fatalf("fixture emitted %d facts; the gate needs a representative stream", firstCount)
	}
}

// TestContentVersionFixtureCoversEveryParser keeps the content-version gate
// honest as parsers are added (review F2 on #7978): every parser the default
// registry dispatches to must parse at least one file of
// testdata/content_version, or a change to that parser could ship without a
// version bump and leave unchanged repositories on old facts. The list comes
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
		t.Fatalf("testdata/content_version has no file for parser keys %v: add one per key, then bump "+
			"gitGenerationContentVersion only if existing output changed, and re-pin with -update-content-version-pin", missing)
	}
}

// contentVersionFixtureDigest copies testdata/content_version to a temp root,
// snapshots it with the production parser engine, builds its fact stream, and
// returns a digest of the facts with the temp root, content-entity ids, and
// indexed_at removed.
func contentVersionFixtureDigest(t *testing.T) (string, int) {
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
	copyContentVersionFixture(t, "testdata/content_version", root)
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
	lines := make([]string, 0, len(envelopes))
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
		lines = append(lines, contentVersionIndexedAt.ReplaceAllString(line, ""))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return fmt.Sprintf("%x", sum), len(lines)
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
