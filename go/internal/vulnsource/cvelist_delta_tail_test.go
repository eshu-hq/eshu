// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"strings"
	"testing"
)

// 5.2 delta tail for eshu-hq/eshu#10354: upstream cves/deltaLog.json is a
// newest-first array of {fetchTime, numberOfChanges, new[], updated[],
// error[]} batches (verified live 2026-11-07). The tail consumes entries
// newer than the stored fetchTime cursor; dirty shards derive from each
// entry's githubLink path (cves/<year>/<NNNxxx>/...).
const cveListDeltaTailFixture = `[
  {
    "fetchTime": "2026-11-07T23:29:09.281Z",
    "numberOfChanges": 1,
    "new": [],
    "updated": [
      {
        "cveId": "CVE-2026-83844",
        "cveOrgLink": "https://www.cve.org/CVERecord?id=CVE-2026-83844",
        "githubLink": "https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/83xxx/CVE-2026-83844.json",
        "dateUpdated": "2026-11-07T23:26:38.481Z"
      }
    ],
    "error": []
  },
  {
    "fetchTime": "2026-11-07T23:23:16.258Z",
    "numberOfChanges": 2,
    "new": [
      {
        "cveId": "CVE-2026-90001",
        "cveOrgLink": "https://www.cve.org/CVERecord?id=CVE-2026-90001",
        "githubLink": "https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/90xxx/CVE-2026-90001.json",
        "dateUpdated": "2026-11-07T23:20:00.000Z"
      }
    ],
    "updated": [
      {
        "cveId": "CVE-2024-12001",
        "cveOrgLink": "https://www.cve.org/CVERecord?id=CVE-2024-12001",
        "githubLink": "https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2024/12xxx/CVE-2024-12001.json",
        "dateUpdated": "2026-11-07T23:21:25.145Z"
      }
    ],
    "error": []
  },
  {
    "fetchTime": "2026-11-07T23:15:00.000Z",
    "numberOfChanges": 0,
    "new": [],
    "updated": [],
    "error": []
  }
]`

func TestCVEListDeltaTailStopsAtCursor(t *testing.T) {
	t.Parallel()

	// The cursor names the newest fully-processed batch; only strictly
	// newer entries are consumed.
	batches, reached, err := ParseCVEListDeltaTail(strings.NewReader(cveListDeltaTailFixture), "2026-11-07T23:23:16.258Z", 0)
	if err != nil {
		t.Fatalf("ParseCVEListDeltaTail() err = %v", err)
	}
	if !reached {
		t.Fatal("ParseCVEListDeltaTail() reached = false, want true (cursor batch present)")
	}
	if len(batches) != 1 {
		t.Fatalf("ParseCVEListDeltaTail() = %d batches, want 1 (only newer than cursor)", len(batches))
	}
	if batches[0].FetchTime != "2026-11-07T23:29:09.281Z" {
		t.Fatalf("ParseCVEListDeltaTail() batch = %q, want the newest entry", batches[0].FetchTime)
	}
}

func TestCVEListDeltaTailReportsGapWhenCursorMissing(t *testing.T) {
	t.Parallel()

	// A cursor older than every entry in the fetched window means the log
	// rolled past it (30-day window): refuse to advance, require re-seed.
	_, reached, err := ParseCVEListDeltaTail(strings.NewReader(cveListDeltaTailFixture), "2026-09-01T00:00:00.000Z", 0)
	if err != nil {
		t.Fatalf("ParseCVEListDeltaTail() err = %v, want nil with reached=false", err)
	}
	if reached {
		t.Fatal("ParseCVEListDeltaTail() reached = true, want false (cursor predates the window)")
	}
}

func TestCVEListDeltaTailRejectsCountMismatch(t *testing.T) {
	t.Parallel()

	bad := `[{"fetchTime": "2026-11-07T23:29:09.281Z", "numberOfChanges": 5, "new": [], "updated": [], "error": []}]`
	if _, _, err := ParseCVEListDeltaTail(strings.NewReader(bad), "", 0); err == nil {
		t.Fatal("ParseCVEListDeltaTail() err = nil, want count-mismatch failure")
	}
}

func TestCVEListDeltaTailRejectsErrorEntries(t *testing.T) {
	t.Parallel()

	bad := `[{"fetchTime": "2026-11-07T23:29:09.281Z", "numberOfChanges": 1, "new": [], "updated": [], "error": [{"cveId": "CVE-2026-00001"}]}]`
	if _, _, err := ParseCVEListDeltaTail(strings.NewReader(bad), "", 0); err == nil {
		t.Fatal("ParseCVEListDeltaTail() err = nil, want error-entry failure")
	}
}

func TestCVEListDeltaTailBatchCapBoundsDecode(t *testing.T) {
	t.Parallel()

	// The cap bounds one poll's decode: exactly maxBatches entries succeed,
	// the (maxBatches+1)th fails the whole read before it is decoded.
	one := `{"fetchTime": "2026-11-07T23:29:09.281Z", "numberOfChanges": 0, "new": [], "updated": [], "error": []}`
	two := `{"fetchTime": "2026-11-07T23:23:16.258Z", "numberOfChanges": 0, "new": [], "updated": [], "error": []}`
	three := `{"fetchTime": "2026-11-07T23:15:00.000Z", "numberOfChanges": 0, "new": [], "updated": [], "error": []}`
	if batches, _, err := ParseCVEListDeltaTail(strings.NewReader("["+one+","+two+"]"), "", 2); err != nil || len(batches) != 2 {
		t.Fatalf("ParseCVEListDeltaTail() = %d batches, err = %v, want 2 batches within the cap", len(batches), err)
	}
	if _, _, err := ParseCVEListDeltaTail(strings.NewReader("["+one+","+two+","+three+"]"), "", 2); err == nil {
		t.Fatal("ParseCVEListDeltaTail() err = nil, want cap failure past maxBatches")
	}
}

func TestCVEListDeltaTailRejectsTrailingBytes(t *testing.T) {
	t.Parallel()

	// Bytes after the top-level array are part of the fetched window and
	// fail the read like any other malformed input; trailing whitespace
	// stays accepted.
	one := `{"fetchTime": "2026-11-07T23:29:09.281Z", "numberOfChanges": 0, "new": [], "updated": [], "error": []}`
	if _, _, err := ParseCVEListDeltaTail(strings.NewReader("["+one+"] TRAILING"), "", 0); err == nil {
		t.Fatal("ParseCVEListDeltaTail() err = nil, want trailing-bytes failure")
	}
	if _, _, err := ParseCVEListDeltaTail(strings.NewReader("["+one+"]  \n"), "", 0); err != nil {
		t.Fatalf("ParseCVEListDeltaTail() err = %v, want trailing whitespace accepted", err)
	}
}

func TestCVEListTailDirtyShardsDeriveFromGithubLinks(t *testing.T) {
	t.Parallel()

	batches, _, err := ParseCVEListDeltaTail(strings.NewReader(cveListDeltaTailFixture), "", 0)
	if err != nil {
		t.Fatalf("ParseCVEListDeltaTail() err = %v", err)
	}
	shards, skipped := CVEListTailDirtyShards(batches)
	want := []string{"cves/2024/12xxx", "cves/2026/83xxx", "cves/2026/90xxx"}
	if len(shards) != len(want) {
		t.Fatalf("CVEListTailDirtyShards() = %v, want %v", shards, want)
	}
	for i := range want {
		if shards[i] != want[i] {
			t.Fatalf("CVEListTailDirtyShards() = %v, want %v", shards, want)
		}
	}
	if skipped != 0 {
		t.Fatalf("CVEListTailDirtyShards() skipped = %d, want 0", skipped)
	}
}

func TestCVEListTailDirtyShardsSkipsMalformedLinks(t *testing.T) {
	t.Parallel()

	batches := []CVEListDeltaBatch{{
		FetchTime: "2026-11-07T23:29:09.281Z",
		Updated: []CVEListDeltaRef{
			{ID: "CVE-2026-83844", GithubLink: "https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/83xxx/CVE-2026-83844.json"},
			{ID: "CVE-2026-00002", GithubLink: "not-a-link"},
			{ID: "CVE-2026-00003", GithubLink: "https://example.com/cves/2026/83xxx"},
		},
	}}
	shards, skipped := CVEListTailDirtyShards(batches)
	if len(shards) != 1 || shards[0] != "cves/2026/83xxx" {
		t.Fatalf("CVEListTailDirtyShards() = %v, want [cves/2026/83xxx]", shards)
	}
	if skipped != 2 {
		t.Fatalf("CVEListTailDirtyShards() skipped = %d, want 2", skipped)
	}
}
