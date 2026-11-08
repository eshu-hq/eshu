// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CVE List midnight-baseline store for eshu-hq/eshu#10354 (Phase 5 5.2).
// The baseline (CVE List midnight zip with its sha256 and cutoff) cannot be
// a #603 cache artifact (643 MB vs the 16 MiB inline cap), so it is a small
// disk store: the zip streams to disk sha256-verified, retention keeps the
// latest and the previous baseline, and shards read per-member without
// loading whole (mirror ADR §3.1-3.3). This file is pure store mechanics:
// no network, no database. Fetch policy (which release asset, retry, rate
// limits) belongs to the caller driving the cadence.

// cveListBaselineDirPrefix names baseline directories: baseline-<date>
// where date is the midnight the zip snapshots (YYYY-MM-DD).
const cveListBaselineDirPrefix = "baseline-"

// cveListBaselineCursorFile is the mirror-cursor sidecar next to the store.
const cveListBaselineCursorFile = "cursor.json"

// VerifyCVEListBaselineStream hashes r as it reads and reports a mismatch
// against the release-asset digest. Callers verify while downloading so a
// 643 MB baseline is never held twice.
func VerifyCVEListBaselineStream(r io.Reader, wantSHA256 string) error {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return fmt.Errorf("vulnsource: baseline stream read: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(wantSHA256) {
		return fmt.Errorf("vulnsource: baseline sha256 mismatch (download corrupt or wrong asset)")
	}
	return nil
}

// PruneCVEListBaselines removes baseline-<date> directories older than the
// two newest (latest plus previous). Entries that are not directories, or
// whose date suffix is not a valid YYYY-MM-DD, are never touched. It
// returns the removed directory names, sorted.
func PruneCVEListBaselines(storeDir string) (removed []string, err error) {
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		return nil, fmt.Errorf("vulnsource: list baseline store: %w", err)
	}
	var dated []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		day, ok := strings.CutPrefix(entry.Name(), cveListBaselineDirPrefix)
		if !ok || !validBaselineDay(day) {
			continue
		}
		dated = append(dated, entry.Name())
	}
	sort.Strings(dated)
	for _, name := range dated[:max(0, len(dated)-2)] {
		if err := os.RemoveAll(filepath.Join(storeDir, name)); err != nil {
			return removed, fmt.Errorf("vulnsource: prune baseline %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	sort.Strings(removed)
	return removed, nil
}

func validBaselineDay(day string) bool {
	if len(day) != len("2006-01-02") {
		return false
	}
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return false
	}
	return t.Format("2006-01-02") == day
}

// CVEListBaselineShardMembers lists the record members of one shard
// directory (cves/<year>/<NNNxxx>) inside the baseline zip, sorted. Only
// the zip directory is scanned: record bodies stay on disk until the
// caller opens them.
func CVEListBaselineShardMembers(zipPath, shard string) ([]string, error) {
	zr, err := openZipReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	prefix := strings.TrimSuffix(shard, "/") + "/"
	var members []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		members = append(members, f.Name)
	}
	sort.Strings(members)
	return members, nil
}

// CVEListBaselineMemberCount counts the CVE-record file members of the
// baseline zip. The mirror record-count check compares this against the
// collected mirror count — never against release-notes prose counts.
func CVEListBaselineMemberCount(zipPath string) (int, error) {
	zr, err := openZipReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = zr.Close() }()
	count := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(f.Name, "cves/") || !strings.HasSuffix(f.Name, ".json") {
			continue
		}
		count++
	}
	return count, nil
}

func openZipReader(zipPath string) (*zip.ReadCloser, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("vulnsource: open baseline zip: %w", err)
	}
	return zr, nil
}

// CVEListMirrorCursor is the durable 5.2 mirror position: the newest
// fully-processed deltaLog fetchTime plus the baseline it applies to. The
// cursor advances only to fetchTimes the tail actually processed; a tail
// read that never reached the cursor refuses to advance (NeedsReseed).
type CVEListMirrorCursor struct {
	// FetchTime is the newest fully-processed deltaLog entry fetchTime.
	FetchTime string `json:"fetch_time"`
	// BaselineDate is the midnight the baseline snapshots (YYYY-MM-DD).
	BaselineDate string `json:"baseline_date"`
	// BaselineSHA256 is the verified baseline zip digest
	// (cache_snapshot_digest on the source snapshot).
	BaselineSHA256 string `json:"baseline_sha256"`
}

// Advance returns the cursor moved to newFetchTime. Both times must parse;
// the result keeps the newer of the two, so a stale batch can never move
// the cursor backwards.
func (c CVEListMirrorCursor) Advance(newFetchTime string) CVEListMirrorCursor {
	current, err := time.Parse(time.RFC3339Nano, c.FetchTime)
	if err != nil {
		current = time.Time{}
	}
	next, err := time.Parse(time.RFC3339Nano, newFetchTime)
	if err != nil {
		return c
	}
	if next.After(current) {
		c.FetchTime = newFetchTime
	}
	return c
}

// NeedsReseed reports whether a tail read demands a fresh midnight
// baseline: true when the read never reached the cursor (the rolling log
// window moved past it). Callers must re-seed, never jump the cursor.
func (c CVEListMirrorCursor) NeedsReseed(reachedCursor bool) bool {
	return !reachedCursor
}

// Lag measures mirror lag as now minus the cursor fetchTime, classified
// against the freshness window. Operators read it through the source
// freshness state; the cadence check is lag within the window (10-15 min
// per the mirror ADR). An unparseable cursor fetchTime fails: lag from an
// unknown position is unknown, never zero.
func (c CVEListMirrorCursor) Lag(now time.Time, window time.Duration) (time.Duration, FreshnessState, error) {
	fetchTime, err := time.Parse(time.RFC3339Nano, c.FetchTime)
	if err != nil {
		return 0, "", fmt.Errorf("vulnsource: unparseable cursor fetchTime %q: %w", c.FetchTime, err)
	}
	lag := now.Sub(fetchTime)
	if lag < 0 {
		lag = 0
	}
	if lag <= window {
		return lag, FreshnessFresh, nil
	}
	return lag, FreshnessStale, nil
}

// WriteCVEListMirrorCursor persists the cursor atomically (write temp,
// rename) with owner-only permissions.
func WriteCVEListMirrorCursor(storeDir string, cur CVEListMirrorCursor) error {
	raw, err := json.Marshal(cur)
	if err != nil {
		return fmt.Errorf("vulnsource: marshal mirror cursor: %w", err)
	}
	tmp, err := os.CreateTemp(storeDir, "cursor-*.tmp")
	if err != nil {
		return fmt.Errorf("vulnsource: stage mirror cursor: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort staging cleanup; harmless once Rename commits tmpName.
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("vulnsource: write mirror cursor: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("vulnsource: close mirror cursor: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("vulnsource: protect mirror cursor: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(storeDir, cveListBaselineCursorFile)); err != nil {
		return fmt.Errorf("vulnsource: commit mirror cursor: %w", err)
	}
	return nil
}

// ReadCVEListMirrorCursor loads the persisted cursor. found is false when
// no cursor was ever written (fresh mirror: seed the baseline, then store
// the newest observed fetchTime).
func ReadCVEListMirrorCursor(storeDir string) (cur CVEListMirrorCursor, found bool, err error) {
	raw, err := os.ReadFile(filepath.Join(storeDir, cveListBaselineCursorFile))
	if err != nil {
		if os.IsNotExist(err) {
			return CVEListMirrorCursor{}, false, nil
		}
		return CVEListMirrorCursor{}, false, fmt.Errorf("vulnsource: read mirror cursor: %w", err)
	}
	if err := json.Unmarshal(raw, &cur); err != nil {
		return CVEListMirrorCursor{}, false, fmt.Errorf("vulnsource: parse mirror cursor: %w", err)
	}
	return cur, true, nil
}
