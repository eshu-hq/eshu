// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkNoteRuntimeDeclarationCandidateNonPin measures the per-content-file
// cost the stream pays for every file that is not a version-pin file, which is
// nearly every file in a repository: one base-name map lookup and no
// allocation.
func BenchmarkNoteRuntimeDeclarationCandidateNonPin(b *testing.B) {
	paths := make([]string, 1024)
	for i := range paths {
		paths[i] = fmt.Sprintf("services/svc%d/internal/handler_%d.go", i%32, i)
	}
	candidates := map[string]Candidate{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NoteRuntimeDeclarationCandidate(candidates, paths[i%len(paths)], "package x\n")
	}
	if len(candidates) != 0 {
		b.Fatalf("non-pin paths were recorded: %d", len(candidates))
	}
}

// corpusEnv names a directory of real package.json and go.mod files. The
// corpus benchmarks skip when it is unset, so CI never depends on a host path.
const corpusEnv = "ESHU_DECLARATION_CORPUS_DIR"

// loadManifestCorpus reads every package.json and go.mod under the corpus
// directory and returns synthetic repository-relative paths with the bodies.
func loadManifestCorpus(b *testing.B) (paths, bodies []string) {
	b.Helper()
	root := os.Getenv(corpusEnv)
	if root == "" {
		b.Skipf("%s is not set", corpusEnv)
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if name := entry.Name(); name != "package.json" && name != "go.mod" {
			return nil
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- benchmark corpus chosen by the operator running it
		if readErr != nil {
			return nil
		}
		// A unique directory per file: the corpus is usually a dependency tree,
		// whose own path would (rightly) be refused as a dependency directory.
		rel := fmt.Sprintf("c%d/%s", len(paths), entry.Name())
		paths, bodies = append(paths, rel), append(bodies, string(body))
		return nil
	})
	if err != nil || len(paths) == 0 {
		b.Skipf("no manifests under %s (err %v)", root, err)
	}
	return paths, bodies
}

// BenchmarkNoteRuntimeDeclarationCandidateCorpus measures the whole per-file
// cost of the content-walk hook over a real corpus of package.json and go.mod
// files (point ESHU_DECLARATION_CORPUS_DIR at one). Before manifests were read
// the hook paid one base-name lookup per file; the same benchmark runs against
// that code for the before figure. The reported metric is nanoseconds and bytes
// per file, not per pass.
func BenchmarkNoteRuntimeDeclarationCandidateCorpus(b *testing.B) {
	paths, bodies := loadManifestCorpus(b)
	total := 0
	for _, body := range bodies {
		total += len(body)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates := map[string]Candidate{}
		for j, path := range paths {
			NoteRuntimeDeclarationCandidate(candidates, path, bodies[j])
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(paths)), "ns/file")
	b.ReportMetric(float64(total)/float64(len(paths)), "bytes/file")
}

// BenchmarkNoteRuntimeDeclarationCandidatePackageJSON measures one typical
// package.json: read at the walk, recorded with its declarations.
func BenchmarkNoteRuntimeDeclarationCandidatePackageJSON(b *testing.B) {
	body := "{\n  \"name\": \"api\",\n  \"version\": \"1.0.0\",\n  \"scripts\": {\"build\": \"tsc\", \"test\": \"jest\"},\n" +
		"  \"engines\": {\"node\": \">=18 <21\", \"npm\": \"^10\"},\n  \"volta\": {\"node\": \"20.11.1\"},\n" +
		"  \"packageManager\": \"pnpm@9.1.0+sha512.0123456789abcdef\",\n  \"dependencies\": {\"express\": \"^4.18.2\", \"zod\": \"^3.22.4\"}\n}\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates := map[string]Candidate{}
		NoteRuntimeDeclarationCandidate(candidates, "services/api/package.json", body)
		if len(candidates) != 1 {
			b.Fatal("package.json was not recorded")
		}
	}
}

// BenchmarkNoteAndEmitRuntimeDeclarationCorpus times the whole per-repository
// cost over the same corpus: the content-walk hook for every file and then the
// emission of every fact it found, through a real fact-stream writer on a
// buffered channel that the pass drains. Emission, not the hook, is most of
// the cost (about 10 us per fact: envelope identity and payload encoding), so
// this is the figure to quote for a repository, and it scales with the number
// of declarations, not the number of files.
func BenchmarkNoteAndEmitRuntimeDeclarationCorpus(b *testing.B) {
	paths, bodies := loadManifestCorpus(b)
	ch := make(chan facts.Envelope, 4*len(paths))
	writer := model.NewFactStreamWriter(ch, nil, "bench")
	observed := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	emitted := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates := map[string]Candidate{}
		for j, path := range paths {
			NoteRuntimeDeclarationCandidate(candidates, path, bodies[j])
		}
		EmitRuntimeDeclarationFactsForCandidates(writer, "repo", "scope", "gen", observed, candidates, nil)
		emitted = len(ch)
		for len(ch) > 0 {
			<-ch
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(paths)), "ns/file")
	b.ReportMetric(float64(emitted), "facts/pass")
	if emitted > 0 {
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(emitted), "ns/fact")
	}
}
