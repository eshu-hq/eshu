// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
)

// nearCapPackageJSON builds a valid package.json just under the manifest byte
// cap whose declarations sit at the front of the body. A string that aliased
// the body would keep the whole backing array alive.
func nearCapPackageJSON() string {
	var b strings.Builder
	size := runtimedeclaration.MaxManifestBytesPerFile - 1024
	b.Grow(size + 256)
	b.WriteString("{\n  \"engines\": {\"node\": \">=18 <21\", \"npm\": \"^10\"},\n  \"volta\": {\"node\": \"20.11.1\"},\n")
	b.WriteString("  \"packageManager\": \"pnpm@9.1.0\",\n  \"dependencies\": {\n")
	for i := 0; b.Len() < size; i++ {
		b.WriteString("    \"dep-")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\": \"^1.0.0\",\n")
	}
	b.WriteString("    \"last\": \"1.0.0\"\n  }\n}\n")
	return b.String()
}

// nearCapGoMod builds a go.mod just under the manifest byte cap with the go and
// toolchain directives at the front.
func nearCapGoMod() string {
	var b strings.Builder
	size := runtimedeclaration.MaxManifestBytesPerFile - 1024
	b.Grow(size + 256)
	b.WriteString("module example.com/big\n\ngo 1.22.1\n\ntoolchain go1.22.3\n\nrequire (\n")
	for i := 0; b.Len() < size; i++ {
		b.WriteString("\texample.com/some/module/path")
		b.WriteString(strings.Repeat("x", i%9))
		b.WriteString(" v1.2.3 // indirect\n")
	}
	b.WriteString(")\n")
	return b.String()
}

// nearCapDockerfile builds a Dockerfile just under the manifest byte cap with
// one versioned FROM at the front and comment padding after it.
func nearCapDockerfile() string {
	var b strings.Builder
	size := runtimedeclaration.MaxManifestBytesPerFile - 1024
	b.Grow(size + 256)
	b.WriteString("FROM node:20.11.1\n")
	for i := 0; b.Len() < size; i++ {
		b.WriteString("# padding ")
		b.WriteString(strings.Repeat("x", i%11))
		b.WriteString("\n")
	}
	return b.String()
}

// nearCapWorkflow builds a workflow just under the manifest byte cap with one
// setup-node version input at the front and comment padding after it.
func nearCapWorkflow() string {
	var b strings.Builder
	size := runtimedeclaration.MaxManifestBytesPerFile - 1024
	b.Grow(size + 256)
	b.WriteString("jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: 20\n")
	for i := 0; b.Len() < size; i++ {
		b.WriteString("# padding ")
		b.WriteString(strings.Repeat("x", i%11))
		b.WriteString("\n")
	}
	return b.String()
}

// nearCapComposerJSON keeps a PHP declaration beside a large ignored field.
func nearCapComposerJSON() string {
	const prefix = `{"require":{"php":"^8.2"},"padding":"`
	const suffix = `"}`
	return prefix + strings.Repeat("x", runtimedeclaration.MaxManifestBytesPerFile-len(prefix)-len(suffix)-64) + suffix
}

// noteNearCapManifests notes count near-cap manifest files at distinct paths
// and returns the total body bytes. Every body is local to this call, so the
// caller holds no reference to one once it returns.
//
//go:noinline
func noteNearCapManifests(candidates map[string]Candidate, count int) (bodyBytes int) {
	for i := 0; i < count; i++ {
		body, name := nearCapPackageJSON(), fmt.Sprintf("m%d/package.json", i)
		switch i % 5 {
		case 1:
			body, name = nearCapGoMod(), fmt.Sprintf("m%d/go.mod", i)
		case 2:
			body, name = nearCapDockerfile(), fmt.Sprintf("m%d/Dockerfile", i)
		case 3:
			// The workflow gate admits only .github/workflows/<name>.yml,
			// so the rotation varies the file name, not the directory.
			body, name = nearCapWorkflow(), fmt.Sprintf(".github/workflows/ci-%d.yml", i)
		case 4:
			body, name = nearCapComposerJSON(), fmt.Sprintf("m%d/composer.json", i)
		}
		bodyBytes += len(body)
		NoteRuntimeDeclarationCandidate(candidates, name, body)
	}
	return bodyBytes
}

// liveHeap returns the live heap in bytes after two collections.
func liveHeap() uint64 {
	var stats runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody proves the state the
// walk keeps for a manifest does not pin the manifest body. Thirty near-cap
// manifests (about 30 MiB of bodies) are noted and their bodies dropped; the
// live heap must grow by a small multiple of the declarations, not by the body
// sizes. An extracted string that is a substring of the body keeps the whole
// backing array reachable, so a missing copy fails this test. It runs serially
// (no t.Parallel) so no other test allocates between the two heap reads.
func TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody(t *testing.T) {
	const manifests = 30
	candidates := map[string]Candidate{}
	before := liveHeap()
	bodyBytes := noteNearCapManifests(candidates, manifests)
	after := liveHeap()
	runtime.KeepAlive(candidates)

	if len(candidates) != manifests {
		t.Fatalf("candidates = %d, want %d", len(candidates), manifests)
	}
	retained := int64(after) - int64(before)
	// The real state is a map entry and a few short strings per manifest; a
	// single retained body is 1 MiB.
	limit := int64(manifests) * (16 << 10)
	t.Logf("noted %d manifests, %d body bytes; live heap grew %d bytes (limit %d)", manifests, bodyBytes, retained, limit)
	if retained > limit {
		t.Fatalf("live heap grew %d bytes holding %d manifests whose bodies total %d bytes, want at most %d: an extracted string aliases its body",
			retained, manifests, bodyBytes, limit)
	}
}
