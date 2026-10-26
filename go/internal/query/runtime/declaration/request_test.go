// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"net/http"
	"strings"
	"testing"
)

func TestListRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	repoIDs := make([]string, maxRepoIDs+1)
	for i := range repoIDs {
		repoIDs[i] = `"repository:r_` + strings.Repeat("a", 3) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `"`
	}
	cases := map[string]string{
		"not json":                  `{`,
		"unknown field":             `{"runtim":"node"}`,
		"trailing data":             `{"runtime":"node"} {}`,
		"major_below no runtime":    `{"major_below":20}`,
		"version_below no runtime":  `{"version_below":"20"}`,
		"both comparators":          `{"runtime":"node","major_below":20,"version_below":"20"}`,
		"negative major_below":      `{"runtime":"node","major_below":-1}`,
		"version_below text":        `{"runtime":"node","version_below":"twenty"}`,
		"version_below four parts":  `{"runtime":"node","version_below":"1.2.3.4"}`,
		"version_below empty part":  `{"runtime":"node","version_below":"1..3"}`,
		"version_below range text":  `{"runtime":"node","version_below":">=18"}`,
		"version_below oversized":   `{"runtime":"node","version_below":"99999999999999999999"}`,
		"zero limit":                `{"limit":0}`,
		"limit over max":            `{"limit":201}`,
		"blank repo id":             `{"repo_ids":["repository:r_a"," "]}`,
		"too many repo ids":         `{"repo_ids":[` + strings.Join(repoIDs, ",") + `]}`,
		"cursor not base64":         `{"cursor":"!!!"}`,
		"cursor not a key":          `{"cursor":"e30"}`,
		"cursor wrong version":      `{"cursor":"` + mustCursor(t, cursorKey{Version: 9}) + `"}`,
		"runtime blank-padded only": `{"runtime":"   ","major_below":20}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := &fakeGraph{}
			w := post(newMux(graph), body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
			if len(graph.calls) != 0 {
				t.Fatalf("a rejected request read the graph %d times, want 0", len(graph.calls))
			}
		})
	}
}

func TestParseVersionBelow(t *testing.T) {
	t.Parallel()

	good := map[string][]int{"20": {20}, "20.10": {20, 10}, "20.10.3": {20, 10, 3}, "0": {0}, "v20": nil}
	for text, want := range good {
		bounds, err := parseVersionBelow(text)
		if want == nil {
			if err == nil {
				t.Errorf("parseVersionBelow(%q) accepted a leading v", text)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseVersionBelow(%q) error = %v", text, err)
			continue
		}
		if len(bounds) != len(want) {
			t.Errorf("parseVersionBelow(%q) = %v, want %v", text, bounds, want)
			continue
		}
		for i := range want {
			if bounds[i] != want[i] {
				t.Errorf("parseVersionBelow(%q) = %v, want %v", text, bounds, want)
			}
		}
	}
}

func TestCursorRoundTripAndBounds(t *testing.T) {
	t.Parallel()

	key := cursorKey{Version: cursorVersion, RepoID: "repository:r_a", RelativePath: "pkg/.nvmrc", LineNumber: 3, Runtime: "node", OrderIndex: 2}
	got, err := decodeCursor(mustCursor(t, key))
	if err != nil {
		t.Fatalf("decodeCursor() error = %v", err)
	}
	if got != key {
		t.Fatalf("cursor round trip = %+v, want %+v", got, key)
	}
	if _, err := decodeCursor(strings.Repeat("A", maxCursorLength+1)); err == nil {
		t.Fatal("decodeCursor accepted an oversized cursor")
	}
}

func mustCursor(t *testing.T, key cursorKey) string {
	t.Helper()
	encoded, err := encodeCursor(key)
	if err != nil {
		t.Fatalf("encodeCursor() error = %v", err)
	}
	return encoded
}
