// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
	"time"
)

// emitManifest runs EmitFile and returns the envelope count, the skip and the
// bytes allocated. The tests assert allocation ceilings and a generous wall
// time bound, never a speed claim.
func emitManifest(path, body string) (int, Skip, uint64) {
	var envelopes int
	var skip Skip
	alloc := totalAllocDelta(func() {
		got, s := EmitFile(testContext(path), "repo-1", path, body)
		envelopes, skip = len(got), s
	})
	return envelopes, skip, alloc
}

// hostilePackageJSON builds a valid package.json of about size bytes: a large
// dependencies map around the two declarations that matter.
func hostilePackageJSON(size int) string {
	var b strings.Builder
	b.Grow(size + 256)
	b.WriteString("{\n  \"engines\": {\"node\": \">=18\"},\n  \"dependencies\": {\n")
	for i := 0; b.Len() < size; i++ {
		b.WriteString("    \"dep-")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\": \"^1.0.0\",\n")
	}
	b.WriteString("    \"last\": \"1.0.0\"\n  },\n  \"volta\": {\"node\": \"20.11.1\"}\n}\n")
	return b.String()
}

func TestEmitPackageJSONAtTheByteCapReadsFlatMemory(t *testing.T) {
	body := hostilePackageJSON(MaxManifestBytesPerFile - 1024)
	if len(body) > MaxManifestBytesPerFile {
		t.Fatalf("test body is %d bytes, over the cap", len(body))
	}
	start := time.Now()
	n, skip, alloc := emitManifest("package.json", body)
	if n != 2 || skip.Skipped() {
		t.Fatalf("emitted %d envelopes, skip %+v, want the two declarations", n, skip)
	}
	// Flat in the body: the same declarations in a tiny file cost about the
	// same. The difference is the scan itself, which allocates for no skipped
	// value.
	_, _, small := emitManifest("package.json", `{"engines":{"node":">=18"},"volta":{"node":"20.11.1"}}`)
	if alloc > small+16<<10 {
		t.Fatalf("reading a %d byte package.json allocated %d bytes against %d for a tiny one, want within 16 KiB", len(body), alloc, small)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("reading a %d byte package.json took %s", len(body), elapsed)
	}
}

func TestEmitManifestOverTheByteCapAllocatesNothingProportionalToTheBody(t *testing.T) {
	for _, path := range []string{"package.json", "go.mod"} {
		body := strings.Repeat("x", 10<<20)
		n, skip, alloc := emitManifest(path, body)
		if n != 0 || skip.Reason != SkipReasonByteCap || skip.ByteSize != len(body) || skip.ByteCap != MaxManifestBytesPerFile {
			t.Fatalf("%s: emitted %d, skip %+v, want a byte-cap skip", path, n, skip)
		}
		if alloc > 16<<10 {
			t.Fatalf("%s: skipping a 10 MiB body allocated %d bytes, want <= 16 KiB", path, alloc)
		}
	}
}

func TestEmitPackageJSONDeepNestingFailsClosedEarly(t *testing.T) {
	for name, body := range map[string]string{
		"arrays":  strings.Repeat("[", 500_000),
		"objects": strings.Repeat(`{"a":`, 100_000),
		"mixed":   `{"engines":{"node":"18"},"x":` + strings.Repeat("[{\"a\":", 50_000),
	} {
		if len(body) > MaxManifestBytesPerFile {
			body = body[:MaxManifestBytesPerFile]
		}
		n, skip, alloc := emitManifest("package.json", body)
		if n != 0 || skip.Reason != SkipReasonManifestUnparseable {
			t.Errorf("%s: emitted %d, skip %+v, want an unparseable skip and no fact", name, n, skip)
		}
		if alloc > 32<<10 {
			t.Errorf("%s: nesting allocated %d bytes, want <= 32 KiB (depth bound %d)", name, alloc, maxJSONDepth)
		}
	}
}

func TestEmitPackageJSONNestingAtTheBoundStillReads(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxJSONDepth-2) + "1" + strings.Repeat("}", maxJSONDepth-2)
	body := `{"config":` + deep + `,"engines":{"node":"18"}}`
	if got := emitDecoded(t, "package.json", body); len(got) != 1 {
		t.Fatalf("nesting within the bound emitted %d facts, want 1", len(got))
	}
	tooDeep := strings.Repeat(`{"a":`, maxJSONDepth+2) + "1" + strings.Repeat("}", maxJSONDepth+2)
	n, skip, _ := emitManifest("package.json", `{"config":`+tooDeep+`,"engines":{"node":"18"}}`)
	if n != 0 || skip.Reason != SkipReasonManifestUnparseable {
		t.Fatalf("nesting past the bound emitted %d, skip %+v, want unparseable", n, skip)
	}
}

func TestEmitPackageJSONRepeatedKeysDoNotAmplifyAllocation(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for b.Len() < MaxManifestBytesPerFile-64 {
		b.WriteString(`"engines":{"node":"18"},"packageManager":"npm@10.0.0",`)
	}
	b.WriteString(`"engines":{"node":"20"}}`)
	n, skip, alloc := emitManifest("package.json", b.String())
	if n != 2 || skip.Skipped() {
		t.Fatalf("emitted %d envelopes, skip %+v, want engines.node and packageManager (last occurrence)", n, skip)
	}
	if alloc > 64<<10 {
		t.Fatalf("%d repeated keys allocated %d bytes, want <= 64 KiB (no per-repeat allocation)", b.Len()/60, alloc)
	}
}

func TestEmitPackageJSONEscapedKeyFloodStaysBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for b.Len() < MaxManifestBytesPerFile-64 {
		b.WriteString(`"engines":{"node":"18"},`)
	}
	b.WriteString(`"engines":{"node":"20"}}`)
	n, skip, alloc := emitManifest("package.json", b.String())
	if n != 1 || skip.Skipped() {
		t.Fatalf("emitted %d envelopes, skip %+v, want 1", n, skip)
	}
	// Each escaped key decodes into a short string: the cost is linear in the
	// body, bounded by the manifest cap, with no retained state.
	if alloc > 24<<20 {
		t.Fatalf("an escaped-key flood allocated %d bytes for a 1 MiB body, want <= 24 MiB", alloc)
	}
}

func TestEmitPackageJSONLongEscapedStringsAreNotDecoded(t *testing.T) {
	longKey := strings.Repeat(`A`, 40)
	longValue := strings.Repeat(`1`, 200)
	body := `{"` + longKey + `":1,"engines":{"node":"` + longValue + `","npm":"10"}}`
	envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", body)
	if len(envelopes) != 1 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
		t.Fatalf("emitted %d, skip %+v, want npm and one rejected over-long escaped value", len(envelopes), skip)
	}
}

func TestEmitPackageJSONInvalidUTF8IsNeverEmitted(t *testing.T) {
	t.Parallel()

	// Invalid bytes in a value the scan skips do not matter; in a declared
	// value they fail the guard, so no U+FFFD-laden raw_spec is invented.
	skipped := "{\"description\":\"caf\xe9 \xff\xfe\",\"engines\":{\"node\":\"18\"}}"
	if got := emitDecoded(t, "package.json", skipped); len(got) != 1 || got[0].RawSpec != "18" {
		t.Fatalf("invalid bytes in a skipped string = %+v, want engines.node 18", got)
	}
	declared := "{\"engines\":{\"node\":\">=18\xff\"}}"
	envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", declared)
	if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected {
		t.Fatalf("invalid UTF-8 in a declared value emitted %d, skip %+v, want a rejection", len(envelopes), skip)
	}
}

func TestEmitGoModHostileBodiesStayBounded(t *testing.T) {
	cases := map[string]string{
		"one huge line":     "go " + strings.Repeat("1", MaxManifestBytesPerFile-8) + "\n",
		"many open parens":  strings.Repeat("require (\n", 100_000),
		"many go lines":     strings.Repeat("go 1.22\n", 100_000),
		"quote soup":        strings.Repeat("\"`\"`//", 150_000),
		"nul bytes":         "go 1.22\x00\n" + strings.Repeat("\x00", 100_000),
		"only comments":     strings.Repeat("// go 1.22\n", 90_000),
		"toolchain flood":   strings.Repeat("toolchain go1.22.3\n", 50_000),
		"crlf and no final": strings.Repeat("require x v1\r\n", 70_000) + "go 1.22",
	}
	for name, body := range cases {
		if len(body) > MaxManifestBytesPerFile {
			body = body[:MaxManifestBytesPerFile]
		}
		start := time.Now()
		n, skip, alloc := emitManifest("go.mod", body)
		if alloc > 4<<20 {
			t.Errorf("%s: allocated %d bytes, want <= 4 MiB", name, alloc)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("%s: took %s", name, elapsed)
		}
		if n > 2 {
			t.Errorf("%s: emitted %d facts from a go.mod, want at most 2 (go and toolchain)", name, n)
		}
		_ = skip
	}
}
