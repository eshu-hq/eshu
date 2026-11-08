// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// CVE List v5 delta tail for eshu-hq/eshu#10354 (Phase 5 5.2). Upstream
// cves/deltaLog.json is a newest-first array of {fetchTime,
// numberOfChanges, new[], updated[], error[]} batches (verified live
// 2026-11-07; ~36 MB rolling 30 days). The tail stream-parses entries
// newest-first and stops at the stored fetchTime cursor — it never loads
// the file whole (mirror ADR §3.3 "stream, do not hold"). Dirty shards
// derive from each entry's githubLink path (cves/<year>/<NNNxxx>/...).
// This file is pure parsing and derivation: no network, no database.

// cveListDeltaTailBatchCap bounds one poll's decode when the caller passes
// maxBatches <= 0. Steady-state 10-15 min polls see a handful of batches;
// hitting the cap means the window outgrew the poll and the operator raises
// it deliberately rather than silently truncating the tail.
const cveListDeltaTailBatchCap = 10000

// CVEListDeltaRef is one changed CVE record inside a delta batch.
type CVEListDeltaRef struct {
	// ID is the CVE identifier (cveId).
	ID string
	// GithubLink is the raw record URL embedding the shard path.
	GithubLink string
}

// CVEListDeltaBatch is one parsed deltaLog entry newer than the cursor.
type CVEListDeltaBatch struct {
	// FetchTime is the entry's fetchTime verbatim.
	FetchTime string
	// New and Updated carry the entry's new[] and updated[] refs.
	New, Updated []CVEListDeltaRef
}

type cveListDeltaWireRef struct {
	CVEID      string `json:"cveId"`
	GithubLink string `json:"githubLink"`
}

type cveListDeltaWireBatch struct {
	FetchTime       string                `json:"fetchTime"`
	NumberOfChanges int                   `json:"numberOfChanges"`
	New             []cveListDeltaWireRef `json:"new"`
	Updated         []cveListDeltaWireRef `json:"updated"`
	Error           []json.RawMessage     `json:"error"`
}

// ParseCVEListDeltaTail decodes deltaLog batches newest-first and returns
// the entries strictly newer than cursorFetchTime. reachedCursor is true
// when the log still contains the cursor batch (no gap) or when
// cursorFetchTime is empty (fresh seed: nothing to catch up). A cursor the
// window has rolled past reports reachedCursor false: the caller must
// re-seed from a fresh midnight baseline, never jump the cursor. A batch
// whose numberOfChanges disagrees with len(new)+len(updated), a non-empty
// error[], an unparseable fetchTime, or more than maxBatches entries fails
// the whole read (nil batches): no partial cursor advance.
func ParseCVEListDeltaTail(r io.Reader, cursorFetchTime string, maxBatches int) (batches []CVEListDeltaBatch, reachedCursor bool, err error) {
	if maxBatches <= 0 {
		maxBatches = cveListDeltaTailBatchCap
	}
	var cursor time.Time
	if cursorFetchTime != "" {
		cursor, err = time.Parse(time.RFC3339Nano, cursorFetchTime)
		if err != nil {
			return nil, false, fmt.Errorf("vulnsource: unparseable delta cursor %q: %w", cursorFetchTime, err)
		}
	}
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return nil, false, fmt.Errorf("vulnsource: delta tail is not a JSON array: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, false, fmt.Errorf("vulnsource: delta tail top level is not an array")
	}
	for dec.More() {
		// Bound the decode work itself: refuse the (maxBatches+1)th entry
		// before decoding it, so one poll never decodes past its budget.
		if len(batches) >= maxBatches {
			return nil, false, fmt.Errorf("vulnsource: delta tail exceeds %d batches without reaching the cursor", maxBatches)
		}
		var wire cveListDeltaWireBatch
		if err := dec.Decode(&wire); err != nil {
			return nil, false, fmt.Errorf("vulnsource: delta tail batch decode: %w", err)
		}
		fetchTime, err := time.Parse(time.RFC3339Nano, wire.FetchTime)
		if err != nil {
			return nil, false, fmt.Errorf("vulnsource: unparseable batch fetchTime %q: %w", wire.FetchTime, err)
		}
		if len(wire.Error) != 0 {
			return nil, false, fmt.Errorf("vulnsource: delta batch %s carries %d error entries", wire.FetchTime, len(wire.Error))
		}
		if wire.NumberOfChanges != len(wire.New)+len(wire.Updated) {
			return nil, false, fmt.Errorf("vulnsource: delta batch %s declares %d changes but carries %d refs",
				wire.FetchTime, wire.NumberOfChanges, len(wire.New)+len(wire.Updated))
		}
		if cursorFetchTime != "" && !fetchTime.After(cursor) {
			return batches, true, nil
		}
		batch := CVEListDeltaBatch{FetchTime: wire.FetchTime}
		for _, ref := range wire.New {
			batch.New = append(batch.New, CVEListDeltaRef{ID: ref.CVEID, GithubLink: ref.GithubLink})
		}
		for _, ref := range wire.Updated {
			batch.Updated = append(batch.Updated, CVEListDeltaRef{ID: ref.CVEID, GithubLink: ref.GithubLink})
		}
		batches = append(batches, batch)
	}
	// The array must close here: bytes after the top level are part of the
	// fetched window and fail the read like any other malformed input.
	// Trailing whitespace is fine (Token skips it before reporting EOF).
	if tok, err := dec.Token(); err != nil {
		return nil, false, fmt.Errorf("vulnsource: delta tail closing delimiter: %w", err)
	} else if delim, ok := tok.(json.Delim); !ok || delim != ']' {
		return nil, false, fmt.Errorf("vulnsource: delta tail top level is not closed")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false, fmt.Errorf("vulnsource: delta tail carries trailing bytes after the array")
	}
	if cursorFetchTime == "" {
		return batches, true, nil
	}
	return batches, false, nil
}

// CVEListTailDirtyShards reduces tail-batch refs to the sorted unique shard
// directories that need re-collection. Shards derive from the ref's
// githubLink path (…/cves/<year>/<NNNxxx>/CVE-….json); refs without a
// well-formed shard segment are skipped and reported via skipped, never
// generated.
func CVEListTailDirtyShards(batches []CVEListDeltaBatch) (shards []string, skipped int) {
	seen := make(map[string]struct{})
	note := func(link string) {
		shard, ok := shardFromGithubLink(link)
		if !ok {
			skipped++
			return
		}
		seen[shard] = struct{}{}
	}
	for _, batch := range batches {
		for _, ref := range batch.New {
			note(ref.GithubLink)
		}
		for _, ref := range batch.Updated {
			note(ref.GithubLink)
		}
	}
	shards = make([]string, 0, len(seen))
	for shard := range seen {
		shards = append(shards, shard)
	}
	sort.Strings(shards)
	return shards, skipped
}

// shardFromGithubLink extracts cves/<year>/<NNNxxx> from a raw record URL.
func shardFromGithubLink(link string) (string, bool) {
	idx := strings.Index(link, "/cves/")
	if idx < 0 {
		return "", false
	}
	rest := strings.Split(link[idx+len("/cves/"):], "/")
	if len(rest) < 3 {
		return "", false
	}
	year, group := rest[0], rest[1]
	if len(year) != 4 || !isDigits(year) {
		return "", false
	}
	if !strings.HasSuffix(group, "xxx") || !isDigits(strings.TrimSuffix(group, "xxx")) {
		return "", false
	}
	return "cves/" + year + "/" + group, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
