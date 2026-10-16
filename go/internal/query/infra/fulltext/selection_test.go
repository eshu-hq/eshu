// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
)

func TestParseReadMode(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{"": false, "off": false, "on": true, " on ": true, "OFF": false, "On": true} {
		got, err := fulltext.ParseReadMode(in)
		if err != nil || got != want {
			t.Errorf("ParseReadMode(%q) = %v, %v; want %v, nil", in, got, err, want)
		}
	}
	for _, in := range []string{"true", "1", "yes", "enabled"} {
		if _, err := fulltext.ParseReadMode(in); err == nil {
			t.Errorf("ParseReadMode(%q) error = nil, want error", in)
		}
	}
}

func TestSelectMatrix(t *testing.T) {
	t.Parallel()
	ready := fulltext.State{Ready: true, Index: fulltext.IndexOnline, Reason: fulltext.ReasonReady}
	cases := []struct {
		name       string
		enabled    bool
		neo4j      bool
		query      string
		state      fulltext.State
		wantPath   fulltext.Path
		wantReason fulltext.Reason
	}{
		{"ready", true, true, "aws_iam_role", ready, fulltext.PathFulltext, fulltext.ReasonReady},
		{"flag off", false, true, "aws_iam_role", ready, fulltext.PathLegacy, fulltext.ReasonFlagOff},
		{"nornicdb", true, false, "aws_iam_role", ready, fulltext.PathLegacy, fulltext.ReasonBackend},
		{"empty query", true, true, "", ready, fulltext.PathLegacy, fulltext.ReasonNoQuery},
		{"never probed", true, true, "x", fulltext.State{}, fulltext.PathLegacy, fulltext.ReasonNotReady},
		{"probe error", true, true, "x", fulltext.State{Index: fulltext.IndexUnknown, Reason: fulltext.ReasonNotReady}, fulltext.PathLegacy, fulltext.ReasonNotReady},
		{"index populating", true, true, "x", fulltext.State{Index: fulltext.IndexPopulating, Reason: fulltext.ReasonIndexNotOnline}, fulltext.PathLegacy, fulltext.ReasonIndexNotOnline},
		{"unkeyed", true, true, "x", fulltext.State{Index: fulltext.IndexOnline, Unkeyed: 3, Reason: fulltext.ReasonUnkeyed}, fulltext.PathLegacy, fulltext.ReasonUnkeyed},
		{"nornicdb wins over flag", false, false, "x", ready, fulltext.PathLegacy, fulltext.ReasonBackend},
		{"query at the bound", true, true, strings.Repeat("😀", fulltext.MaxQueryRunes), ready, fulltext.PathFulltext, fulltext.ReasonReady},
		{"query past the bound", true, true, strings.Repeat("a", fulltext.MaxQueryRunes+1), ready, fulltext.PathLegacy, fulltext.ReasonQueryTooLong},
		{"long query with flag off", false, true, strings.Repeat("a", fulltext.MaxQueryRunes+1), ready, fulltext.PathLegacy, fulltext.ReasonFlagOff},
	}
	for _, c := range cases {
		path, reason := fulltext.Select(c.enabled, c.neo4j, c.query, c.state)
		if path != c.wantPath || reason != c.wantReason {
			t.Errorf("%s: Select() = %s/%s, want %s/%s", c.name, path, reason, c.wantPath, c.wantReason)
		}
	}
}
