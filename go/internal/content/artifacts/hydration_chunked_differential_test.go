// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// perFileFetch is the reader of the needed-content change before batching
// (#7126 PR 1): one GetFileContent statement per file, eight at a time. It is
// the oracle of the batched reader.
func perFileFetch(
	ctx context.Context,
	reader Reader,
	repoID string,
	hydrated []File,
	indexes []int,
	_ *ReadStats,
) error {
	single, ok := reader.(singleFileReader)
	if !ok {
		return fmt.Errorf("per-file reader %T has no single-file read", reader)
	}
	return hydrateFileIndexes(ctx, single, repoID, hydrated, indexes)
}

// perFilePasses runs the needed-content plan with the per-file reader.
var perFilePasses = loadPasses{
	hydrate: configHydrationPolicy{
		readsContent:  configArtifactReadsContent,
		planResources: planKustomizeResourceReads,
		fetch:         perFileFetch,
	}.hydrate,
	build: BuildConfigPaths,
}

// requireBatchedMatchesPerFile loads the same tree through the batched reader
// and through the per-file reader and requires the same output, the same
// outcome class, and the same set of paths fetched.
func requireBatchedMatchesPerFile(
	t testing.TB,
	name string,
	newStore func() *hydrationStore,
	anchor func(testing.TB, *hydrationStore) []File,
) (perFile, batched loadOutcome, perFileStore, batchedStore *hydrationStore) {
	t.Helper()
	perFileStore, batchedStore = newStore(), newStore()
	perFile = runConfigLoad(t, perFileStore, anchor(t, perFileStore), perFilePasses)
	batched = runConfigLoad(t, batchedStore, anchor(t, batchedStore), productionLoadPasses)
	if perFile.digest != batched.digest || (perFile.err == nil) != (batched.err == nil) {
		t.Fatalf("%s: batched reader differs from the per-file reader: per-file rows=%d err=%v digest=%.12s, batched rows=%d err=%v digest=%.12s",
			name, perFile.rows, perFile.err, perFile.digest, batched.rows, batched.err, batched.digest)
	}
	if perFile.err == nil {
		want, got := uniqueSorted(perFileStore.getCalls), uniqueSorted(batchedStore.getCalls)
		if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
			t.Fatalf("%s: batched reader fetched a different set of paths\n  per-file %v\n  batched  %v", name, want, got)
		}
	}
	return perFile, batched, perFileStore, batchedStore
}

func listedAnchor(t testing.TB, store *hydrationStore) []File {
	t.Helper()
	return store.listed(t)
}

// TestBatchedHydrationMatchesPerFileOnGeneratedTrees runs the 400 seeded trees
// of the needed-content proof through the real loader with the batched reader
// and with the per-file reader and compares the digest of config_paths, the
// outcome and the fetched path set.
func TestBatchedHydrationMatchesPerFileOnGeneratedTrees(t *testing.T) {
	t.Parallel()

	nonEmpty, batchedStatements, perFileStatements := 0, 0, 0
	for seed := int64(1); seed <= 400; seed++ {
		files := randomTree(rand.New(rand.NewSource(seed)))
		name := fmt.Sprintf("seed %d", seed)
		perFile, batched, perFileStore, batchedStore := requireBatchedMatchesPerFile(t, name,
			func() *hydrationStore { return newHydrationStore(files...) },
			listedAnchor)
		if perFile.rows > 0 {
			nonEmpty++
		}
		if batchedStore.singleCalls != 0 {
			t.Fatalf("%s: batched reader issued %d GetFileContent statements", name, batchedStore.singleCalls)
		}
		if len(batchedStore.batchCalls) > perFileStore.singleCalls {
			t.Fatalf("%s: batched reader issued %d statements for %d per-file reads", name, len(batchedStore.batchCalls), perFileStore.singleCalls)
		}
		if batched.stats.FilesHydrated != perFile.stats.FilesHydrated || batched.stats.FilesSkipped != perFile.stats.FilesSkipped {
			t.Fatalf("%s: hydrated/skipped %d/%d, per-file %d/%d", name,
				batched.stats.FilesHydrated, batched.stats.FilesSkipped, perFile.stats.FilesHydrated, perFile.stats.FilesSkipped)
		}
		batchedStatements += len(batchedStore.batchCalls)
		perFileStatements += perFileStore.singleCalls
	}
	if nonEmpty < 100 {
		t.Fatalf("only %d of 400 generated trees produced rows; the generator does not exercise the extractors", nonEmpty)
	}
	if batchedStatements >= perFileStatements {
		t.Fatalf("batched statements %d, per-file %d: batching removed nothing", batchedStatements, perFileStatements)
	}
}

// TestBatchedHydrationChunkScenariosMatchPerFile covers the chunk-shaped edge
// cases the generated trees reach only by chance.
func TestBatchedHydrationChunkScenariosMatchPerFile(t *testing.T) {
	t.Parallel()

	blank := func(files []File, from, to int) []File {
		for i := from; i < to; i++ {
			files[i].Content = " \n\t"
		}
		return files
	}
	cases := map[string]struct {
		files   []File
		mutate  func(*hydrationStore)
		wantErr bool
	}{
		"0 candidates":                     {files: []File{fileRow("README.md", "x")}},
		"1 candidate":                      {files: tfFiles(1)},
		"10 candidates":                    {files: tfFiles(10)},
		"11 candidates":                    {files: tfFiles(11)},
		"50 candidates":                    {files: tfFiles(50)},
		"60 candidates, 10 beyond the cap": {files: tfFiles(60)},
		"a chunk of all blank rows":        {files: blank(tfFiles(30), 10, 20)},
		"a chunk of mixed blank rows":      {files: blank(tfFiles(30), 12, 17)},
		"a vanished row in the middle chunk": {
			files:  tfFiles(30),
			mutate: func(s *hydrationStore) { s.gone["infra/m015/main.tf"] = true },
		},
		"a vanished first row": {
			files:  tfFiles(30),
			mutate: func(s *hydrationStore) { s.gone["infra/m000/main.tf"] = true },
		},
		"every row of a chunk vanished": {
			files: tfFiles(30),
			mutate: func(s *hydrationStore) {
				for i := 10; i < 20; i++ {
					s.gone[fmt.Sprintf("infra/m%03d/main.tf", i)] = true
				}
			},
		},
		"an erroring chunk among good ones": {
			files:   tfFiles(40),
			mutate:  func(s *hydrationStore) { s.getAt["infra/m025/main.tf"] = errInjectedRead },
			wantErr: true,
		},
		"two erroring chunks": {
			files: tfFiles(40),
			mutate: func(s *hydrationStore) {
				s.getAt["infra/m005/main.tf"] = errInjectedRead
				s.getAt["infra/m035/main.tf"] = errors.New("second failure")
			},
			wantErr: true,
		},
		"huge file in a chunk": {
			files: append(tfFiles(12), fileRow("infra/huge/main.tf",
				"module \"big\" {\n  source = \"./big\"\n}\n"+strings.Repeat("# padding line for a two megabyte value\n", 55_000))),
		},
		"special character paths": {
			files: []File{
				fileRow("a b/main.tf", "module \"a\" {\n  source = \"./a\"\n}\n"),
				fileRow(`q"uote/main.tf`, "module \"q\" {\n  source = \"./q\"\n}\n"),
				fileRow(`back\slash/main.tf`, "module \"b\" {\n  source = \"./b\"\n}\n"),
				fileRow("100%_x/main.tf", "module \"p\" {\n  source = \"./p\"\n}\n"),
				fileRow("ü-é/main.tf", "module \"u\" {\n  source = \"./u\"\n}\n"),
				fileRow("comma,brace{}/main.tf", "module \"c\" {\n  source = \"./c\"\n}\n"),
				fileRow("it's/main.tf", "module \"i\" {\n  source = \"./i\"\n}\n"),
			},
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			newStore := func() *hydrationStore {
				store := newHydrationStore(tc.files...)
				if tc.mutate != nil {
					tc.mutate(store)
				}
				return store
			}
			perFile, batched, _, _ := requireBatchedMatchesPerFile(t, name, newStore, listedAnchor)
			if tc.wantErr {
				if perFile.err == nil || batched.err == nil {
					t.Fatalf("per-file err=%v batched err=%v, want both to fail", perFile.err, batched.err)
				}
				if !errors.Is(batched.err, errInjectedRead) && name == "an erroring chunk among good ones" {
					t.Fatalf("batched error = %v, want it to wrap the injected read failure", batched.err)
				}
				return
			}
			if perFile.err != nil || batched.err != nil {
				t.Fatalf("per-file err=%v batched err=%v, want none", perFile.err, batched.err)
			}
			// The scenarios that read files must produce rows, or the
			// comparison is vacuous.
			if name != "0 candidates" && !strings.Contains(name, "all blank") && !strings.Contains(name, "every row of a chunk vanished") &&
				perFile.rows == 0 {
				t.Fatalf("%s produced no rows; the equality check is vacuous", name)
			}
		})
	}
}

// TestBatchedHydrationDuplicateListPathsMatchPerFile gives the anchor list two
// rows with one path. The per-file reader reads the path twice; the batched
// reader reads it once and gives the row to both.
func TestBatchedHydrationDuplicateListPathsMatchPerFile(t *testing.T) {
	t.Parallel()

	newStore := func() *hydrationStore { return newHydrationStore(tfFiles(12)...) }
	withDuplicates := func(t testing.TB, s *hydrationStore) []File {
		files := s.listed(t)
		return append(files, files[3], files[4], files[3])
	}
	perFile, _, _, batchedStore := requireBatchedMatchesPerFile(t, "duplicate list rows", newStore, withDuplicates)
	if perFile.rows == 0 {
		t.Fatal("fixture produced no rows; the equality check is vacuous")
	}
	if got, want := len(batchedStore.batchCalls), 2*2; got > want {
		t.Fatalf("batched statements = %d, want at most %d", got, want)
	}
	for _, call := range batchedStore.batchCalls {
		seen := map[string]bool{}
		for _, path := range call {
			if seen[path] {
				t.Fatalf("chunk %v names %q twice; the statement must carry distinct paths", call, path)
			}
			seen[path] = true
		}
	}
}
