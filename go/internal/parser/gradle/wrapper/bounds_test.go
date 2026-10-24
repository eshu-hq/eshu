// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

import (
	"bytes"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"testing"
)

// allocOf returns the bytes allocated while fn runs. Callers are serial tests.
func allocOf(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func requireCeiling(t *testing.T, what string, got, limit uint64) {
	t.Helper()
	t.Logf("%s = %d (ceiling %d)", what, got, limit)
	if got > limit {
		t.Fatalf("%s = %d, want <= %d", what, got, limit)
	}
}

func TestScanOversizeInputIsRefusedWithoutScanning(t *testing.T) {
	source := append([]byte("distributionUrl="), bytes.Repeat([]byte("x"), 10<<20)...)
	var result Result
	allocated := allocOf(func() { result = Scan(source) })
	if result.State != StateOversize || len(result.Rows) != 0 {
		t.Fatalf("State=%q rows=%d, want oversize with none", result.State, len(result.Rows))
	}
	requireCeiling(t, "bytes allocated for a 10 MB line", allocated, 4<<10)
}

func TestScanLargeInputsStayBoundedByTheFieldBounds(t *testing.T) {
	cases := map[string][]byte{
		"one 60 KiB value":           []byte("distributionUrl=" + strings.Repeat("a", 60<<10) + "\n"),
		"60 KiB of continuations":    []byte("distributionUrl=" + strings.Repeat("ab\\\n", 15<<10)),
		"30k other keys":             []byte(strings.Repeat("k=v\n", 15<<10) + "distributionUrl=https\\://h/gradle-8.5-bin.zip\n"),
		"endless escapes":            []byte("distributionUrl=" + strings.Repeat("\\:", 30<<10) + "\n"),
		"backslash run at line end":  []byte("a=" + strings.Repeat("\\", 60<<10) + "\n"),
		"many blank and comment ls":  []byte(strings.Repeat(" \n# c\n", 10<<10)),
		"unterminated final dangler": []byte("distributionUrl=https\\://h/gradle-8.5-bin.zip\\"),
	}
	for name, source := range cases {
		var result Result
		allocated := allocOf(func() { result = Scan(source) })
		if result.State != StateParsed {
			t.Fatalf("%s: State=%q", name, result.State)
		}
		requireCeiling(t, name+" bytes allocated", allocated, 16<<10)
	}
}

func TestScanAllocationDoesNotGrowWithUnrelatedLines(t *testing.T) {
	build := func(n int) []byte { return []byte(strings.Repeat("key=value\n", n)) }
	smallInput, largeInput := build(2000), build(6000)
	small := allocOf(func() { Scan(smallInput) })
	large := allocOf(func() { Scan(largeInput) })
	t.Logf("bytes: 2000 lines %d, 6000 lines %d", small, large)
	if large > small+16<<10 {
		t.Fatalf("allocation grew from %d to %d with unrelated lines: want flat", small, large)
	}
}

// TestScanRandomBytesNeverPanics feeds seeded random byte soup, biased toward
// the characters that matter to a properties reader, and checks that any row
// names a version present in the input.
func TestScanRandomBytesNeverPanics(t *testing.T) {
	rng := rand.New(rand.NewSource(8822))
	alphabet := []byte("distributionUrl=:\\\n\r #!\tgradle-8.5-bin.zip/?@x")
	for round := 0; round < 5000; round++ {
		source := make([]byte, rng.Intn(300))
		for i := range source {
			source[i] = alphabet[rng.Intn(len(alphabet))]
		}
		result := Scan(source)
		for _, row := range result.Rows {
			version, _ := row["value"].(string)
			if version == "" || !bytes.Contains(source, []byte(version)) {
				t.Fatalf("round %d: row value %q is not in the input %q", round, version, source)
			}
		}
	}
}

// TestScanGeneratedWrappersMatchTheGrammarOracle builds wrapper files with a
// known outcome around random unrelated properties that hold a marker, then
// checks the row and that the marker, the userinfo and the host never appear in
// anything the scan returns.
func TestScanGeneratedWrappersMatchTheGrammarOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(88222))
	versions := []string{"8.5", "7.6.4", "8.6-rc-1", "9.0-milestone-2", "8.7-20240101000000+0000"}
	for doc := 0; doc < 3000; doc++ {
		version := versions[rng.Intn(len(versions))]
		kind := []string{"bin", "all"}[rng.Intn(2)]
		userinfo := ""
		if rng.Intn(2) == 0 {
			userinfo = "user-SECRETUSER:pw-SECRETPASS@"
		}
		url := fmt.Sprintf(`https\://%shost-SECRETHOST.example.test/dl/gradle-%s-%s.zip`, userinfo, version, kind)
		wantRow := true
		switch rng.Intn(6) {
		case 0:
			url = strings.Replace(url, ".zip", ".tgz", 1)
			wantRow = false
		case 1:
			url = strings.Replace(url, "gradle-", "gradlew-", 1)
			wantRow = false
		case 2:
			url += `A`
			wantRow = false
		}
		var b strings.Builder
		for i, n := 0, rng.Intn(6); i < n; i++ {
			switch rng.Intn(3) {
			case 0:
				fmt.Fprintf(&b, "other%d=SECRETVALUE%d\n", i, i)
			case 1:
				fmt.Fprintf(&b, "# note SECRETNOTE%d\n", i)
			default:
				fmt.Fprintf(&b, "multi%d=SECRETA\\\n   SECRETB\n", i)
			}
		}
		b.WriteString("distributionUrl=" + url + "\n")
		fmt.Fprintf(&b, "tail=SECRETTAIL\n")
		result := Scan([]byte(b.String()))
		if wantRow != (len(result.Rows) == 1) {
			t.Fatalf("doc %d: rows=%v, want row=%v\n%s", doc, result.Rows, wantRow, b.String())
		}
		if wantRow && (result.Rows[0]["value"] != version || result.Rows[0]["distribution_type"] != kind) {
			t.Fatalf("doc %d: row %v, want version %q type %q\n%s", doc, result.Rows[0], version, kind, b.String())
		}
		if rendered := fmt.Sprintf("%#v", result); strings.Contains(rendered, "SECRET") || strings.Contains(rendered, "host-") {
			t.Fatalf("doc %d: result leaks input: %s", doc, rendered)
		}
	}
}
