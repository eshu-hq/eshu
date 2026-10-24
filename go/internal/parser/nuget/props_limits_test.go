// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/nuget"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parseDirect calls nuget.Parse on a fresh file so the measured allocation is
// the parser's own, not the engine's. Limit tests are not parallel on purpose:
// Go pauses parallel tests while a sequential test runs, so the process-wide
// allocation counters see only this test.
type directParse struct {
	payload map[string]any
	bytes   uint64
	mallocs uint64
	err     error
}

func parseDirect(t *testing.T, fileName string, body string) directParse {
	t.Helper()

	path := filepath.Join(t.TempDir(), fileName)
	writeTestFile(t, path, body)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	payload, err := nuget.Parse(path, false, shared.Options{})
	runtime.ReadMemStats(&after)
	return directParse{payload: payload, err: err, bytes: after.TotalAlloc - before.TotalAlloc, mallocs: after.Mallocs - before.Mallocs}
}

func requireAllocCeiling(t *testing.T, label string, inputBytes int, gotBytes uint64, factor float64) {
	t.Helper()

	// The ceiling is a multiple of the input size, never wall time. The floor
	// absorbs the runtime's fixed per-call overhead.
	limit := uint64(float64(inputBytes)*factor) + (1 << 20)
	if gotBytes > limit {
		t.Fatalf("%s: TotalAlloc = %d bytes for %d input bytes, want <= %d (%.1fx + 1 MiB)", label, gotBytes, inputBytes, limit, factor)
	}
}

func TestParseCentralPropsOverInputLimitReadsNothing(t *testing.T) {
	huge := `<Project><PropertyGroup><A>` + strings.Repeat("x", 10<<20) + `</A></PropertyGroup></Project>`
	res := parseDirect(t, "Directory.Build.props", huge)
	payload, err, bytes := res.payload, res.err, res.bytes
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil with input_over_limit", err)
	}
	if !limitFlag(t, payload, "input_over_limit") {
		t.Fatal("input_over_limit = false, want true for a 10 MiB single line")
	}
	if rows := variableRows(t, payload); len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for an over-limit file", len(rows))
	}
	// Reading the file (and its line-ending normalisation) is the only linear cost.
	requireAllocCeiling(t, "10 MiB single line", len(huge), bytes, 3)
}

func TestParseCentralPropsOversizedFieldIsCountedNotTruncated(t *testing.T) {
	body := `<Project><ItemGroup>
<PackageVersion Include="Long.Version" Version="` + strings.Repeat("1", 1<<20) + `" />
<PackageVersion Include="Real.One" Version="1.0.0" />
<PackageVersion Include="` + strings.Repeat("N", 300) + `" Version="1.0.0" />
</ItemGroup><PropertyGroup><Big>` + strings.Repeat("v", 1<<20) + `</Big></PropertyGroup></Project>`
	res := parseDirect(t, "Directory.Packages.props", body)
	payload, err, bytes := res.payload, res.err, res.bytes
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := limitCount(t, payload, "oversized_fields"); got != 3 {
		t.Fatalf("oversized_fields = %d, want 3 (version, name, property value)", got)
	}
	onlyRow(t, payload, "PackageVersion", "Real.One")
	if got := len(rowsInSection(t, payload, "PackageVersion")); got != 1 {
		t.Fatalf("PackageVersion rows = %d, want only Real.One", got)
	}
	big := onlyRow(t, payload, "PropertyGroup", "Big")
	if big["value"] != "" || big["value_evidence"] != "oversized" || big["partial_evidence"] != true {
		t.Fatalf("oversized property row = %v, want empty value, oversized, partial", big)
	}
	requireAllocCeiling(t, "oversized fields", len(body), bytes, 12)
}

func TestParseCentralPropsCapsRowsAndCountsOverflow(t *testing.T) {
	const entries = 90000
	var body strings.Builder
	body.WriteString("<Project><ItemGroup>")
	for i := 0; i < entries; i++ {
		fmt.Fprintf(&body, `<PackageVersion Include="p%d" Version="1"/>`, i)
	}
	body.WriteString("</ItemGroup></Project>")
	if body.Len() > nuget.MaxPropsInputBytes {
		t.Fatalf("fixture is %d bytes, over the input cap: shrink it so the row cap is what trips", body.Len())
	}

	res := parseDirect(t, "Directory.Packages.props", body.String())
	payload, err, bytes, mallocs := res.payload, res.err, res.bytes, res.mallocs
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := len(rowsInSection(t, payload, "PackageVersion")); got != nuget.MaxPropsItemRows {
		t.Fatalf("PackageVersion rows = %d, want the cap %d", got, nuget.MaxPropsItemRows)
	}
	if got := limitCount(t, payload, "dropped_item_rows"); got != entries-nuget.MaxPropsItemRows {
		t.Fatalf("dropped_item_rows = %d, want %d", got, entries-nuget.MaxPropsItemRows)
	}
	requireAllocCeiling(t, "90k entries", body.Len(), bytes, 20)
	if perEntry := mallocs / entries; perEntry > 40 {
		t.Fatalf("mallocs per entry = %d, want <= 40", perEntry)
	}
}

func TestParseCentralPropsCapsPropertiesAndKeepsDroppedDeclarationsHonest(t *testing.T) {
	var body strings.Builder
	body.WriteString("<Project><PropertyGroup>")
	for i := 0; i < nuget.MaxPropsPropertyRows+500; i++ {
		fmt.Fprintf(&body, "<P%d>1</P%d>", i, i)
	}
	// Past the row cap, a second differing declaration of an early name must
	// still make that name ambiguous even though its row is dropped.
	body.WriteString("<P0>2</P0>")
	body.WriteString("</PropertyGroup><ItemGroup>")
	body.WriteString(`<PackageVersion Include="Early" Version="$(P0)" />`)
	body.WriteString(`<PackageVersion Include="Kept" Version="$(P1)" />`)
	fmt.Fprintf(&body, `<PackageVersion Include="Dropped" Version="$(P%d)" />`, nuget.MaxPropsPropertyRows+10)
	body.WriteString("</ItemGroup></Project>")

	res := parseDirect(t, "Directory.Build.props", body.String())
	payload, err := res.payload, res.err
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := len(rowsInSection(t, payload, "PropertyGroup")); got != nuget.MaxPropsPropertyRows {
		t.Fatalf("property rows = %d, want the cap %d", got, nuget.MaxPropsPropertyRows)
	}
	if got := limitCount(t, payload, "dropped_property_rows"); got != 501 {
		t.Fatalf("dropped_property_rows = %d, want 501 (500 new names plus the late P0)", got)
	}
	if got := onlyRow(t, payload, "PackageVersion", "Early")["version_evidence"]; got != "ambiguous_msbuild_property" {
		t.Fatalf("Early evidence = %v, want ambiguous_msbuild_property", got)
	}
	if got := onlyRow(t, payload, "PackageVersion", "Kept")["value"]; got != "1" {
		t.Fatalf("Kept value = %v, want 1", got)
	}
	if got := onlyRow(t, payload, "PackageVersion", "Dropped")["version_evidence"]; got != "unresolved_msbuild_property" {
		t.Fatalf("Dropped evidence = %v, want unresolved_msbuild_property: a property past the cap is unproven", got)
	}
}

func TestParseCentralPropsDeepNestingStopsAtTheDepthCap(t *testing.T) {
	deep := `<Project><Unknown>` + strings.Repeat("<a>", 500_000) + strings.Repeat("</a>", 500_000) + `</Unknown></Project>`
	res := parseDirect(t, "Directory.Build.props", deep)
	payload, err, bytes := res.payload, res.err, res.bytes
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil with depth_exceeded", err)
	}
	if !limitFlag(t, payload, "depth_exceeded") {
		t.Fatal("depth_exceeded = false, want true")
	}
	if rows := variableRows(t, payload); len(rows) != 0 {
		t.Fatalf("rows = %d, want 0: a file the scanner abandoned proves nothing", len(rows))
	}
	requireAllocCeiling(t, "500k nested tags", len(deep), bytes, 3)
}

func TestParseCentralPropsUnterminatedConstructsFailWithBoundedAllocation(t *testing.T) {
	filler := strings.Repeat("x", 2<<20)
	for name, body := range map[string]string{
		"comment": `<Project><!-- ` + filler,
		"cdata":   `<Project><PropertyGroup><A><![CDATA[` + filler,
		"tag":     `<Project><ItemGroup><PackageVersion Include="` + filler,
		"text":    `<Project><PropertyGroup><A>` + filler,
	} {
		res := parseDirect(t, "Directory.Packages.props", body)
		err, bytes := res.err, res.bytes
		if err == nil {
			t.Fatalf("%s: err = nil, want an error", name)
		}
		requireAllocCeiling(t, "unterminated "+name, len(body), bytes, 12)
	}
}

// TestParseCentralPropsAllocationGrowsLinearlyWithEntries doubles the entry
// count well past the row cap and requires allocation to at most double: a
// quadratic or retained-per-entry cost would show as a ratio above 2.
func TestParseCentralPropsAllocationGrowsLinearlyWithEntries(t *testing.T) {
	build := func(entries int) string {
		var body strings.Builder
		body.WriteString("<Project><ItemGroup>")
		for i := 0; i < entries; i++ {
			fmt.Fprintf(&body, `<PackageVersion Include="p%d" Version="1"/>`, i)
		}
		body.WriteString("</ItemGroup></Project>")
		return body.String()
	}
	small := parseDirect(t, "Directory.Packages.props", build(30000)).bytes
	large := parseDirect(t, "Directory.Packages.props", build(60000)).bytes
	if ratio := float64(large) / float64(small); ratio > 2.3 {
		t.Fatalf("alloc ratio for 2x entries = %.2f (small=%d large=%d), want <= 2.3", ratio, small, large)
	}
}

// TestParseCentralPropsNameTrackingOverflowDemotes holds the duplicate
// tracker's cap: once more distinct names than it remembers appear, a hidden
// duplicate is possible, so every row of the type is demoted, and memory
// stays bounded by the cap rather than the input.
func TestParseCentralPropsNameTrackingOverflowDemotes(t *testing.T) {
	build := func(n int) string {
		var b strings.Builder
		b.WriteString("<Project><ItemGroup>")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, `<PackageVersion Include="P%d" Version="1.0.0" />`, i)
		}
		b.WriteString(`<GlobalPackageReference Include="G" Version="1.0.0" /></ItemGroup></Project>`)
		return b.String()
	}
	const tracked = 2 * nuget.MaxPropsItemRows

	atCap := parseDirect(t, "Directory.Packages.props", build(tracked-1))
	if atCap.err != nil {
		t.Fatal(atCap.err)
	}
	first := variableRows(t, atCap.payload)[0]
	if hasKey(first, "item_ambiguity") {
		t.Fatalf("names within the cap must stay resolved: %#v", first)
	}

	over := parseDirect(t, "Directory.Packages.props", build(tracked+1))
	if over.err != nil {
		t.Fatal(over.err)
	}
	rows := variableRows(t, over.payload)
	if got, _ := rows[0]["item_ambiguity"].(string); got != "duplicate_include" {
		t.Fatalf("first PackageVersion item_ambiguity = %q, want duplicate_include past the cap", got)
	}
	if got := limitCount(t, over.payload, "dropped_item_rows"); got != tracked+2-nuget.MaxPropsItemRows {
		t.Fatalf("dropped_item_rows = %d, want %d", got, tracked+2-nuget.MaxPropsItemRows)
	}
}

// TestParseCentralPropsItemOperationsStayBounded feeds just under the input cap
// of the operations the duplicate and Update/Remove tracker records: distinct
// Includes past its name cap, named and wildcard Updates, and Removes.
// Allocation must stay a small multiple of the input. Measured at 15.6x
// (14.1x before the tracker) in a normal build and 27.8x (25.6x before)
// under -race, which inflates every allocation; the 32x ceiling holds both.
func TestParseCentralPropsItemOperationsStayBounded(t *testing.T) {
	var body strings.Builder
	body.WriteString("<Project><ItemGroup>")
	for i := 0; body.Len() < nuget.MaxPropsInputBytes-200; i++ {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&body, `<PackageVersion Include="P%d" Version="1.0.0" />`, i)
		case 1:
			fmt.Fprintf(&body, `<PackageVersion Update="P%d" Version="2.0.0" />`, i-1)
		case 2:
			body.WriteString(`<PackageVersion Update="P*;Q" Version="2.0.0" />`)
		default:
			fmt.Fprintf(&body, `<PackageVersion Remove="P%d" />`, i-3)
		}
	}
	body.WriteString("</ItemGroup></Project>")
	res := parseDirect(t, "Directory.Packages.props", body.String())
	if res.err != nil {
		t.Fatal(res.err)
	}
	if got := len(variableRows(t, res.payload)); got != nuget.MaxPropsItemRows {
		t.Fatalf("rows = %d, want the %d cap", got, nuget.MaxPropsItemRows)
	}
	t.Logf("%d input bytes, %d allocated (%.1fx), %d mallocs", body.Len(), res.bytes, float64(res.bytes)/float64(body.Len()), res.mallocs)
	requireAllocCeiling(t, "item operations", body.Len(), res.bytes, 32)
}
