// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opdigest

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// digestTags is the top-level key order of the Output Shape table in
// docs/public/reference/operator-digest.md, transcribed by hand rather than
// derived from the Digest struct, so that reordering the struct fails here.
//
// Each entry is the whole `json` struct tag, not just the key name. The doc
// says the digest carries these eight fields, so a tag option that lets one
// disappear -- ",omitempty" -- contradicts it as surely as a rename does, and
// only the whole-tag comparison catches that: the emitted key is "limitations"
// either way, and this test's fixture always populates the field.
//
// Transcribed by hand means the lockstep runs one way only: this file catches
// the structs drifting from these arrays, never the doc drifting from either.
// Reorder a row in the contract doc's table and leave the struct alone and the
// test stays green while the doc is wrong. Editing digestTags or artifactTags
// is a three-place change -- the doc row, the array entry, and the struct
// field, in one commit.
var digestTags = []string{
	"schema",
	"scope",
	"profile",
	"truth",
	"sections",
	"suggested_questions",
	"limitations",
	"source_refs",
}

// artifactTags is the top-level key order of the Shareable Artifact table in
// the same doc, transcribed by hand and compared whole for the same reasons.
var artifactTags = []string{
	"schema",
	"digest",
	"artifact",
	"redaction",
	"source_refs",
	"validation",
	"limitations",
}

// TestJSONKeyOrderMatchesContractDoc holds both documents against the contract
// doc's two tables, from two sides: the `json` struct tags in declaration
// order, and the keys the encoder actually emits.
//
// Both sides are needed. The tags alone would not prove what comes out of
// encoding/json; the emitted keys alone miss a tag option, because ",omitempty"
// leaves the key name unchanged and this fixture always populates every field.
//
// The determinism tests nearby cannot do either. They marshal the same input
// twice and compare the two results, so swapping two struct fields leaves them
// green while changing every byte of every artifact anyone has already saved.
func TestJSONKeyOrderMatchesContractDoc(t *testing.T) {
	t.Parallel()

	options, err := OptionsFromFlags("repo:demo/service-api", DefaultProfile, DefaultQuestionMax)
	if err != nil {
		t.Fatalf("OptionsFromFlags: %v", err)
	}
	digest := BuildDigest(options)

	t.Run("digest", func(t *testing.T) {
		t.Parallel()
		assertJSONTags(t, reflect.TypeOf(Digest{}), digestTags)
		data, err := json.Marshal(digest)
		if err != nil {
			t.Fatalf("marshal digest: %v", err)
		}
		assertKeyOrder(t, data, digestTags)
	})

	t.Run("artifact", func(t *testing.T) {
		t.Parallel()
		assertJSONTags(t, reflect.TypeOf(Artifact{}), artifactTags)
		path := filepath.Join(t.TempDir(), "operator-digest.json")
		if err := WriteArtifact(path, digest); err != nil {
			t.Fatalf("WriteArtifact: %v", err)
		}
		assertKeyOrder(t, readArtifactFile(t, path), artifactTags)
	})
}

// assertJSONTags compares typ's fields, in declaration order, against the whole
// `json` tag each one is expected to carry -- options included, so adding
// ",omitempty" or ",string" to a documented field fails here.
//
// Both failure messages print the slices with %q rather than %v. An unexported
// field, or one with no `json` tag, contributes an empty string to got, and %v
// renders that as nothing at all: the two lists then differ only by an
// invisible gap, and the reader cannot see why the test failed.
func assertJSONTags(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	got := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		got = append(got, typ.Field(i).Tag.Get("json"))
	}
	if len(got) != len(want) {
		t.Fatalf("%s json tags = %q, want %q", typ.Name(), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s json tag %d = %q, want %q\n got: %q\nwant: %q", typ.Name(), i, got[i], want[i], got, want)
		}
	}
}

// assertKeyOrder compares the emitted top-level keys against the key names in
// want. It reads through the tag options that assertJSONTags pins, because an
// emitted key never carries them.
func assertKeyOrder(t *testing.T, data []byte, want []string) {
	t.Helper()
	got := topLevelKeys(t, data)
	if len(got) != len(want) {
		t.Fatalf("top-level keys = %v, want %v", got, want)
	}
	for i := range want {
		name, _, _ := strings.Cut(want[i], ",")
		if got[i] != name {
			t.Fatalf("top-level key %d = %q, want %q\n got: %v\nwant: %v", i, got[i], name, got, want)
		}
	}
}

// topLevelKeys returns the object's keys in the order they appear in data.
func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	open, err := decoder.Token()
	if err != nil {
		t.Fatalf("read opening token: %v", err)
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		t.Fatalf("opening token = %v, want {", open)
	}
	keys := make([]string, 0, len(digestTags))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("read key token: %v", err)
		}
		key, ok := token.(string)
		if !ok {
			t.Fatalf("key token = %v, want a string", token)
		}
		keys = append(keys, key)
		var skipped json.RawMessage
		if err := decoder.Decode(&skipped); err != nil {
			t.Fatalf("skip value for key %q: %v", key, err)
		}
	}
	return keys
}
