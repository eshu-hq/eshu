// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"testing"
	"time"
)

// priorAdvisoryInstant is the parse newestCopyInstant and parsePriorityTime
// used before #10741: RFC 3339 only, no second layout. It is kept here as the
// baseline so one binary compares the old and new readers on the same inputs
// and the same machine state.
func priorAdvisoryInstant(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	instant, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return instant, true
}

// advisoryInstantBenchInputs covers each shape the election and the priority
// score read: the zoned shapes every source but NVD emits, the unzoned shape
// every NVD stamp carries today, and an unparseable value.
var advisoryInstantBenchInputs = []struct {
	name string
	raw  string
}{
	{"zoned_z", "2026-08-11T19:33:44Z"},
	{"zoned_z_fraction", "2026-08-11T19:33:44.513123456Z"},
	{"zoned_offset", "2026-08-11T19:33:44+01:00"},
	{"unzoned_millis", "2026-08-11T19:33:44.513"},
	{"garbage", "not-a-time"},
}

// BenchmarkAdvisoryInstantParse compares the prior RFC 3339-only parse with
// parseAdvisoryInstant (eshu-hq/eshu#10741). The zoned rows must not regress,
// because every GHSA, OSV, and CVE List stamp takes that path. The unzoned row
// is the cost of reading the NVD stamps that previously failed to parse.
func BenchmarkAdvisoryInstantParse(b *testing.B) {
	for _, in := range advisoryInstantBenchInputs {
		b.Run("prior/"+in.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = priorAdvisoryInstant(in.raw)
			}
		})
		b.Run("shared/"+in.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parseAdvisoryInstant(in.raw)
			}
		})
	}
}
