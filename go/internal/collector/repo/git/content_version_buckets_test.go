// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/epoch"
)

const contentVersionFixtureRoot = "testdata/content_version"

// contentVersionPathKeys are the payload keys that name the fixture file a
// fact describes, in lookup order. source_metadata.path (documentation facts)
// is read after them.
var contentVersionPathKeys = []string{"relative_path", "content_path", "workflow_path"}

// contentVersionFact is one normalized fact line and the ledger bucket it is
// attributed to.
type contentVersionFact struct {
	Kind   string
	Bucket string
	Line   string
	// Missed lists fixture paths the fact's payload names that the bucket does
	// not: attribution picked a different file or the catch-all.
	Missed []string
}

// contentVersionFixtureInputs returns the sha256 of every fixture file keyed
// by its slash-separated relative path, plus the epoch.RepositoryBucket entry:
// a digest of the whole tree listing and bytes.
func contentVersionFixtureInputs(t *testing.T) map[string]string {
	t.Helper()
	inputs := map[string]string{}
	err := filepath.WalkDir(contentVersionFixtureRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(contentVersionFixtureRoot, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path) // #nosec G304 -- test fixture under testdata
		if err != nil {
			return err
		}
		inputs[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(body))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", contentVersionFixtureRoot, err)
	}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	tree := sha256.New()
	for _, name := range names {
		_, _ = fmt.Fprintf(tree, "%s\x00%s\n", name, inputs[name])
	}
	inputs[epoch.RepositoryBucket] = fmt.Sprintf("%x", tree.Sum(nil))
	return inputs
}

// contentVersionBucket attributes one fact payload to the fixture file it
// describes, or to epoch.RepositoryBucket when it names none. roots are the
// absolute checkout paths to strip. It also returns every fixture path the
// payload names at the top level or in source_metadata that differs from the
// chosen bucket, so the totality test can catch a missed attribution key.
func contentVersionBucket(payload map[string]any, roots []string, inputs map[string]string) (string, []string) {
	bucket := ""
	for _, key := range contentVersionPathKeys {
		if name := contentVersionFixturePath(payload[key], roots, inputs); name != "" {
			bucket = name
			break
		}
	}
	if bucket == "" {
		if metadata, ok := payload["source_metadata"].(map[string]string); ok {
			bucket = contentVersionFixturePath(metadata["path"], roots, inputs)
		}
		if metadata, ok := payload["source_metadata"].(map[string]any); ok && bucket == "" {
			bucket = contentVersionFixturePath(metadata["path"], roots, inputs)
		}
	}
	if bucket == "" {
		bucket = epoch.RepositoryBucket
	}
	var missed []string
	note := func(value any) {
		if name := contentVersionFixturePath(value, roots, inputs); name != "" && name != bucket {
			missed = append(missed, name)
		}
	}
	for _, value := range payload {
		note(value)
	}
	switch metadata := payload["source_metadata"].(type) {
	case map[string]string:
		for _, value := range metadata {
			note(value)
		}
	case map[string]any:
		for _, value := range metadata {
			note(value)
		}
	}
	sort.Strings(missed)
	return bucket, missed
}

// contentVersionFixturePath normalizes value to a fixture-relative path and
// returns it when it names a fixture file, or "" otherwise.
func contentVersionFixturePath(value any, roots []string, inputs map[string]string) string {
	text, ok := value.(string)
	if !ok || text == "" {
		return ""
	}
	for _, root := range roots {
		text = strings.TrimPrefix(text, root+string(filepath.Separator))
	}
	text = strings.TrimPrefix(filepath.ToSlash(text), "./")
	if text == epoch.RepositoryBucket {
		return ""
	}
	if _, ok := inputs[text]; ok {
		return text
	}
	return ""
}

// contentVersionBuckets groups attributed facts into ledger buckets: one per
// bucket that holds at least one fact, with the bucket's input digest, fact
// count, and the sha256 of its sorted lines.
func contentVersionBuckets(t *testing.T, facts []contentVersionFact, inputs map[string]string) []epoch.Bucket {
	t.Helper()
	lines := map[string][]string{}
	for _, fact := range facts {
		lines[fact.Bucket] = append(lines[fact.Bucket], fact.Line)
	}
	buckets := make([]epoch.Bucket, 0, len(lines))
	for name, bucketLines := range lines {
		input, ok := inputs[name]
		if !ok {
			t.Fatalf("bucket %q has no input digest", name)
		}
		sort.Strings(bucketLines)
		buckets = append(buckets, epoch.Bucket{
			Name:         name,
			InputDigest:  input,
			FactCount:    len(bucketLines),
			OutputDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(bucketLines, "\n")))),
		})
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Name < buckets[j].Name })
	return buckets
}

// contentVersionFixtureBuckets is the ledger the current code computes for
// testdata/content_version.
func contentVersionFixtureBuckets(t *testing.T) []epoch.Bucket {
	t.Helper()
	inputs := contentVersionFixtureInputs(t)
	return contentVersionBuckets(t, contentVersionFixtureFacts(t, inputs), inputs)
}

// TestContentVersionBucketsAreTotal is the #9097 totality guard: every emitted
// fact lands in exactly one bucket, the bucket counts sum to the stream, every
// bucket is a fixture file or the catch-all, and no fact names a fixture file
// other than its bucket (a fact attributed to the catch-all while its payload
// names a file means an attribution key is missing).
func TestContentVersionBucketsAreTotal(t *testing.T) {
	t.Parallel()
	inputs := contentVersionFixtureInputs(t)
	facts := contentVersionFixtureFacts(t, inputs)
	buckets := contentVersionBuckets(t, facts, inputs)

	total := 0
	for _, b := range buckets {
		total += b.FactCount
	}
	if total != len(facts) {
		t.Fatalf("bucket counts sum to %d, stream has %d facts", total, len(facts))
	}
	fileBuckets := 0
	for _, b := range buckets {
		if b.Name != epoch.RepositoryBucket {
			fileBuckets++
		}
	}
	if fileBuckets < 2 {
		t.Fatalf("only %d file buckets: attribution is not per file", fileBuckets)
	}
	for _, fact := range facts {
		if len(fact.Missed) > 0 {
			t.Errorf("%s fact in bucket %s also names fixture files %v: add its path key to contentVersionPathKeys",
				fact.Kind, fact.Bucket, fact.Missed)
		}
		if fact.Kind == "file" && fact.Bucket == epoch.RepositoryBucket {
			t.Errorf("a file fact fell into %s: %s", epoch.RepositoryBucket, fact.Line)
		}
	}
}

// TestContentVersionBucketCatchesUnkeyedPath is the seeded violation for the
// totality guard: a payload that names a fixture file under a key the
// attribution does not read is reported as missed, not silently bucketed.
func TestContentVersionBucketCatchesUnkeyedPath(t *testing.T) {
	t.Parallel()
	inputs := map[string]string{"src/app.py": "in", epoch.RepositoryBucket: "tree"}
	bucket, missed := contentVersionBucket(map[string]any{"manifest_path": "/root/src/app.py"}, []string{"/root"}, inputs)
	if bucket != epoch.RepositoryBucket || len(missed) != 1 || missed[0] != "src/app.py" {
		t.Fatalf("unkeyed fixture path: bucket %q missed %v, want catch-all and [src/app.py]", bucket, missed)
	}
	bucket, missed = contentVersionBucket(map[string]any{"content_path": "src/app.py"}, nil, inputs)
	if bucket != "src/app.py" || len(missed) != 0 {
		t.Fatalf("keyed fixture path: bucket %q missed %v, want src/app.py and none", bucket, missed)
	}
}
