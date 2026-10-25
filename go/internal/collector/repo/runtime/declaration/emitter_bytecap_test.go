// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"runtime"
	"strings"
	"testing"
)

// hostileToolVersions builds a .tool-versions body of about size bytes made of
// one-byte version tokens ("nodejs 1 1 1 ..."), the densest shape a hostile
// repository can use to maximise tokens per byte.
func hostileToolVersions(size int) string {
	var b strings.Builder
	b.Grow(size)
	for b.Len() < size {
		b.WriteString("nodejs")
		for k := 0; k < 16 && b.Len() < size; k++ {
			b.WriteString(" 1")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// totalAllocDelta returns the bytes the process allocated while fn ran. The
// callers are not parallel, so no other test allocates while it is measured.
func totalAllocDelta(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestEmitOverTheByteCapAllocatesNothingProportionalToTheBody proves a 10 MB
// pin file is rejected before any tokenizing: the skip path may not allocate
// memory proportional to the body (the pre-fix path allocated about 178x the
// body size before skipping). The ceiling is allocation, never wall time.
func TestEmitOverTheByteCapAllocatesNothingProportionalToTheBody(t *testing.T) {
	body := hostileToolVersions(10 << 20)
	ctx := testContext(".tool-versions")
	var got int
	alloc := totalAllocDelta(func() { got = len(Emit(ctx, "repo-1", ".tool-versions", body)) })
	if got != 0 {
		t.Fatalf("Emit of a 10 MB pin file = %d envelopes, want 0", got)
	}
	const ceiling = 64 << 10
	if alloc > ceiling {
		t.Fatalf("skipping a 10 MB pin file allocated %d bytes, want <= %d (independent of body size)", alloc, ceiling)
	}
}

// TestEmitOverTheTokenCapUnderTheByteCapCountsWithoutStoring proves a file that
// is over MaxTokensPerFile but under the byte cap is skipped without building a
// declaration per token: allocation stays far below one declaration (about 80
// bytes) per token.
func TestEmitOverTheTokenCapUnderTheByteCapCountsWithoutStoring(t *testing.T) {
	body := hostileToolVersions(60 << 10)
	ctx := testContext(".tool-versions")
	var got int
	alloc := totalAllocDelta(func() { got = len(Emit(ctx, "repo-1", ".tool-versions", body)) })
	if got != 0 {
		t.Fatalf("Emit of a 60 KiB pin file over the token cap = %d envelopes, want 0", got)
	}
	// Storing a declaration per token (about 88 bytes each) would cost
	// megabytes at this size; the ceiling is far below that.
	const ceiling = 256 << 10
	if alloc > ceiling {
		t.Fatalf("token-cap skip allocated %d bytes, want <= %d (count-only past the cap)", alloc, ceiling)
	}
}

// TestEmitFileSkipsAFileOverTheByteCapBeforeParsing proves the byte cap is the
// first check: a 10 MB body is skipped whole with the byte reason and size, no
// token count (it was never tokenized), and no envelope.
func TestEmitFileSkipsAFileOverTheByteCapBeforeParsing(t *testing.T) {
	t.Parallel()

	body := hostileToolVersions(10 << 20)
	got, skip := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", body)
	want := Skip{Reason: SkipReasonByteCap, ByteSize: len(body), ByteCap: MaxBytesPerFile}
	if len(got) != 0 || skip != want {
		t.Fatalf("10 MB file: %d envelopes, skip %+v; want 0, %+v", len(got), skip, want)
	}
}

// TestEmitFileByteCapBoundary proves a body of exactly MaxBytesPerFile still
// emits and one byte more is skipped, and that a non-admitted path is never a
// byte-cap skip (it is not a pin file at all).
func TestEmitFileByteCapBoundary(t *testing.T) {
	t.Parallel()

	pad := func(prefix string, size int) string { return prefix + strings.Repeat(" ", size-len(prefix)) }
	atCap := pad("nodejs 20.1.0", MaxBytesPerFile)
	got, skip := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", atCap)
	if len(got) != 1 || skip.Skipped() {
		t.Fatalf("body of %d bytes: %d envelopes, skip %+v; want 1, none", len(atCap), len(got), skip)
	}
	over := atCap + " "
	got, skip = EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", over)
	if len(got) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteSize != MaxBytesPerFile+1 {
		t.Fatalf("body of %d bytes: %d envelopes, skip %+v; want 0, byte cap", len(over), len(got), skip)
	}
	got, skip = EmitFile(testContext(".npmrc"), "repo-1", ".npmrc", over)
	if len(got) != 0 || skip.Skipped() {
		t.Fatalf("non-admitted path: %d envelopes, skip %+v; want 0, none", len(got), skip)
	}
}

// TestEmitFileOneLongTokenIsBoundedByTheByteCap proves a single token cannot
// exceed the file cap: a one-line .nvmrc just under the cap emits one fact whose
// raw_spec is the whole line, and one byte over is skipped, so no raw_spec is
// longer than MaxBytesPerFile.
func TestEmitFileOneLongTokenIsBoundedByTheByteCap(t *testing.T) {
	t.Parallel()

	line := strings.Repeat("x", MaxBytesPerFile)
	got, skip := EmitFile(testContext(".nvmrc"), "repo-1", ".nvmrc", line)
	if len(got) != 1 || skip.Skipped() {
		t.Fatalf("token of %d bytes: %d envelopes, skip %+v; want 1, none", len(line), len(got), skip)
	}
	got, skip = EmitFile(testContext(".nvmrc"), "repo-1", ".nvmrc", line+"x")
	if len(got) != 0 || skip.Reason != SkipReasonByteCap {
		t.Fatalf("token of %d bytes: %d envelopes, skip %+v; want 0, byte cap", len(line)+1, len(got), skip)
	}
}

// TestEmitFileTokenCapSkipKeepsTheExactCount proves the count-only path still
// reports the exact token count for a file under the byte cap, and keeps the
// token reason, so the skip log stays precise.
func TestEmitFileTokenCapSkipKeepsTheExactCount(t *testing.T) {
	t.Parallel()

	body := toolVersionsWithTokens(5000)
	got, skip := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", body)
	want := Skip{Reason: SkipReasonTokenCap, ByteSize: len(body), TokenCount: 5000}
	if len(got) != 0 || skip != want {
		t.Fatalf("5000-token file: %d envelopes, skip %+v; want 0, %+v", len(got), skip, want)
	}
}
