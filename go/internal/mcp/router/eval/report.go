// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Format writes the report as plain text: overall top-1 and top-5, one line per
// family, then every top-1 miss with what the ranker offered instead. The
// output is deterministic so two runs of the same ranker diff cleanly. It
// returns the write error from w, if any.
func Format(w io.Writer, r *Report) error {
	var b strings.Builder
	b.WriteString("MCP router routing accuracy (report-only)\n")
	fmt.Fprintf(&b, "overall: %s\n", counts(r.Tally))
	names := make([]string, 0, len(r.Families))
	for name := range r.Families {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  %-22s %s\n", name, counts(r.Families[name]))
	}
	fmt.Fprintf(&b, "top-1 misses: %d\n", len(r.Misses))
	for _, m := range r.Misses {
		want := m.Expected
		if len(m.Alternates) > 0 {
			want += " (or " + strings.Join(m.Alternates, ", ") + ")"
		}
		place := "not in top 5"
		if m.Rank > 0 {
			place = fmt.Sprintf("found at rank %d", m.Rank)
		}
		fmt.Fprintf(&b, "MISS %s [%s] %q\n     expected %s; %s\n     got %s\n",
			m.ID, m.Family, m.Question, want, place, strings.Join(m.Got, ", "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// counts renders one tally as "top-1 a/n (p%), top-5 b/n (q%)".
func counts(t Tally) string {
	return fmt.Sprintf("top-1 %d/%d (%s), top-5 %d/%d (%s)",
		t.Top1, t.Total, percent(t.Top1, t.Total), t.Top5, t.Total, percent(t.Top5, t.Total))
}

func percent(n, total int) string {
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(total))
}
