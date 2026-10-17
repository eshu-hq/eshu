// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// The generator of the extractor-version guard corpus. It is the only code
// that reads the corpus inputs (tests/fixtures, the recorded S0 corpus and the
// hydration tests' generated trees); the guard reads only the snapshot it
// writes. See the package README, "Extractor version and its guard", for the
// re-snapshot procedure.

package artifacts

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var updateExtractorCorpus = flag.Bool("update-extractor-corpus", false,
	"rewrite testdata/guard/*.jsonl from the corpus inputs (TestRegenerateExtractorCorpus)")

const (
	// guardSnapshotSeeds is the number of randomTree seeds frozen into the
	// generated snapshot.
	guardSnapshotSeeds = 150
	// guardSnapshotMaxBytes keeps each snapshot file under the
	// check-added-large-files hook (--maxkb=1024).
	guardSnapshotMaxBytes = 1000 << 10
	// guardMaxFileBytes is the content_files size bound the S4 corpus keeps.
	guardMaxFileBytes = 256 << 10
)

// TestRegenerateExtractorCorpus rewrites the guard corpus. It runs only with
// -update-extractor-corpus:
//
//	cd go && go test ./internal/content/artifacts/ \
//	    -run TestRegenerateExtractorCorpus -count=1 -args -update-extractor-corpus
//
// Each repository keeps only its config-artifact candidates, and the
// generator fails unless the derive's output over the kept, JSON round-tripped
// files equals its output over every input file, so the snapshot answers as
// the full repository does.
func TestRegenerateExtractorCorpus(t *testing.T) {
	if !*updateExtractorCorpus {
		t.Skip("set -update-extractor-corpus to rewrite testdata/guard")
	}
	fixtures := append(fixtureSnapshotInputs(t), recordedSnapshotInputs(t)...)
	writeGuardSnapshot(t, guardSnapshotFiles[0], freezeGuardRepositories(t, fixtures))
	writeGuardSnapshot(t, guardSnapshotFiles[1], freezeGuardRepositories(t, generatedSnapshotInputs()))
	repos, corpusDigest := readGuardSnapshot(t)
	out := digestGuardOutput(t, repos)
	t.Logf("wrote guard corpus: repositories=%d files=%d rows=%d withRows=%d corpusDigest=%s outputDigest=%s",
		out.Repositories, out.Files, out.Rows, out.WithRows, corpusDigest, out.Digest)
}

// freezeGuardRepositories drops every file that is not a config-artifact
// candidate and proves that the drop and the JSON round trip leave the
// derive's output unchanged.
func freezeGuardRepositories(t *testing.T, inputs []guardRepository) []guardRepository {
	t.Helper()
	frozen := make([]guardRepository, 0, len(inputs))
	for _, repo := range inputs {
		if len(repo.Files) >= ListLimit {
			t.Fatalf("%s holds %d files; the guard corpus must stay below ListLimit (%d) so its output is the full-window derive", repo.Label, len(repo.Files), ListLimit)
		}
		kept := guardRepository{Label: repo.Label}
		for _, f := range repo.Files {
			if len(f.Content) > guardMaxFileBytes {
				t.Fatalf("%s/%s is %d bytes, above the %d-byte content bound", repo.Label, f.RelativePath, len(f.Content), guardMaxFileBytes)
			}
			if !utf8.ValidString(f.Content) || !utf8.ValidString(f.RelativePath) || strings.ContainsRune(f.Content, 0) {
				t.Fatalf("%s/%s cannot be a content_files row (invalid UTF-8 or NUL)", repo.Label, f.RelativePath)
			}
			if isConfigArtifactCandidate(File{RelativePath: f.RelativePath, Content: f.Content}) {
				kept.Files = append(kept.Files, f)
			}
		}
		if len(kept.Files) == 0 {
			if rows := deriveGuardRows(t, repo); len(rows) != 0 {
				t.Fatalf("%s: no candidate file but %d rows", repo.Label, len(rows))
			}
			continue
		}
		sort.Slice(kept.Files, func(i, j int) bool { return kept.Files[i].RelativePath < kept.Files[j].RelativePath })
		encoded, err := json.Marshal(kept)
		if err != nil {
			t.Fatal(err)
		}
		var decoded guardRepository
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if want, got := deriveGuardRows(t, repo), deriveGuardRows(t, decoded); !reflect.DeepEqual(want, got) {
			t.Fatalf("%s: the snapshot's rows differ from the full repository's rows:\nwant %v\n got %v", repo.Label, want, got)
		}
		frozen = append(frozen, decoded)
	}
	sort.Slice(frozen, func(i, j int) bool { return frozen[i].Label < frozen[j].Label })
	return frozen
}

// writeGuardSnapshot writes one repository per line, with no HTML escaping,
// no timestamp and no map, so the bytes are a function of the inputs only.
func writeGuardSnapshot(t *testing.T, name string, repos []guardRepository) {
	t.Helper()
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	for _, repo := range repos {
		if err := encoder.Encode(repo); err != nil {
			t.Fatal(err)
		}
	}
	if buf.Len() > guardSnapshotMaxBytes {
		t.Fatalf("%s would be %d bytes, above %d; lower guardSnapshotSeeds", name, buf.Len(), guardSnapshotMaxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureSnapshotInputs reads tests/fixtures the way the S4 equality test in
// storage/postgres/config/artifacts does: every fixture directory and each of
// its child directories is one repository, and every file up to 256 KiB that
// is valid UTF-8 without a NUL is kept, since only such a file can be a
// content_files row. Paths use forward slashes on every OS.
func fixtureSnapshotInputs(t *testing.T) []guardRepository {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "tests", "fixtures")
	var repos []guardRepository
	add := func(dir string) {
		var files []guardFile
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(dir, p)
			if relErr != nil {
				return relErr
			}
			content, readErr := os.ReadFile(p)
			if readErr != nil || len(content) > guardMaxFileBytes || !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
				return nil
			}
			files = append(files, guardFile{RelativePath: filepath.ToSlash(rel), Content: string(content)})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
		if len(files) == 0 {
			return
		}
		relDir, _ := filepath.Rel(root, dir)
		repos = append(repos, guardRepository{Label: "fx:" + filepath.ToSlash(relDir), Files: files})
	}
	top, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	for _, entry := range top {
		if !entry.IsDir() {
			continue
		}
		add(filepath.Join(root, entry.Name()))
		children, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("read fixtures: %v", err)
		}
		for _, child := range children {
			if child.IsDir() {
				add(filepath.Join(root, entry.Name(), child.Name()))
			}
		}
	}
	return repos
}

// recordedSnapshotInputs reads the 26 recorded file sets of the S0 corpus
// (testdata/name_free_corpus.jsonl).
func recordedSnapshotInputs(t *testing.T) []guardRepository {
	t.Helper()
	cases := nameFreeLoadCorpus(t)
	repos := make([]guardRepository, 0, len(cases))
	for i, c := range cases {
		repos = append(repos, guardRepository{Label: fmt.Sprintf("recorded:%02d", i), Files: toGuardFiles(c.files())})
	}
	return repos
}

// generatedSnapshotInputs returns the hydration tests' mutant corpus (which
// holds the kitchen sink), randomTree seeds 1 to guardSnapshotSeeds, and the
// supplement for the kinds the other inputs emit fewer than three times.
func generatedSnapshotInputs() []guardRepository {
	var repos []guardRepository
	for scenario, files := range mutantCorpus() {
		repos = append(repos, guardRepository{Label: "mutant:" + scenario, Files: toGuardFiles(files)})
	}
	for seed := int64(1); seed <= guardSnapshotSeeds; seed++ {
		files := randomTree(rand.New(rand.NewSource(seed)))
		repos = append(repos, guardRepository{Label: fmt.Sprintf("seed:%03d", seed), Files: toGuardFiles(files)})
	}
	return append(repos, supplementSnapshotInputs()...)
}

// supplementSnapshotInputs are the files added for the guard (#8155): Docker
// Compose secrets and Terragrunt read_terragrunt_config, which tests/fixtures
// and the generated trees emit only once or twice.
func supplementSnapshotInputs() []guardRepository {
	compose := "services:\n" +
		"  api:\n    image: api\n    secrets:\n      - db_password\n      - source: api_key\n        target: key\n" +
		"  worker:\n    image: worker\n    secrets:\n      - db_password\n      - tls_cert\n" +
		"  web:\n    image: web\n    secrets: [tls_cert, oauth]\n" +
		"secrets:\n" +
		"  db_password:\n    file: ./secrets/db_password.txt\n" +
		"  api_key:\n    file: secrets/api.key\n" +
		"  tls_cert:\n    file: ../shared/tls/cert.pem\n" +
		"  oauth:\n    file: ./secrets/oauth.json\n" +
		"  external_one:\n    external: true\n"
	readConfig := func(dir string) guardFile {
		return guardFile{
			RelativePath: dir + "/terragrunt.hcl",
			Content: "locals {\n" +
				"  env    = read_terragrunt_config(find_in_parent_folders(\"env.hcl\"))\n" +
				"  region = read_terragrunt_config(\"${get_terragrunt_dir()}/region.hcl\")\n" +
				"  common = read_terragrunt_config(find_in_parent_folders(\"common.hcl\"))\n" +
				"}\n",
		}
	}
	return []guardRepository{
		{Label: "supplement:compose-secrets", Files: []guardFile{{RelativePath: "deploy/docker-compose.yml", Content: compose}}},
		{Label: "supplement:terragrunt-read-config", Files: []guardFile{
			readConfig("live/prod/app"),
			readConfig("live/stage/app"),
			{RelativePath: "live/env.hcl", Content: "locals {\n  env = \"prod\"\n}\n"},
		}},
	}
}

func toGuardFiles(files []File) []guardFile {
	out := make([]guardFile, 0, len(files))
	for _, f := range files {
		out = append(out, guardFile{RelativePath: f.RelativePath, Content: f.Content})
	}
	return out
}
