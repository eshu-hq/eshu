// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// requirementsUserinfoSecret builds a credential-shaped value by
// concatenation so no source line looks like a committed secret to push
// protection.
func requirementsUserinfoSecret() string {
	return "tok" + "en-" + "value-9f3a"
}

// TestPipRequirementsUserinfoStaysOutOfEntityFacts drives the production path
// (NativeRepositorySnapshotter, then buildStreamingGeneration) over a
// requirements file whose URL and VCS lines carry userinfo, and requires that
// no content_entity fact holds it (#9074). The host and the package name stay;
// VCS refs such as @v1 and branches containing / survive.
//
// The content fact keeps the whole file body. That is the content-store policy
// gap tracked by #8812, shared with constraints.txt, and this test pins it so
// the documented limit cannot drift: the body still holds the text.
func TestPipRequirementsUserinfoStaysOutOfEntityFacts(t *testing.T) {
	t.Parallel()

	secret := requirementsUserinfoSecret()
	body := strings.Join([]string{
		"privpkg @ https://ci-user:" + secret + "@host.example/p.whl",
		"git+https://ci-user:" + secret + "@git.example.com/acme/privvcs.git@v1.0.0#egg=privvcs",
		"git+https://ci-user:" + secret + "@git.example.com/acme/privbranch.git@feature/foo#egg=privbranch",
		"https://ci-user:" + secret + "@files.example.com/bare-1.0.tar.gz",
		"plain==3.0",
		"",
	}, "\n")
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.txt"), []byte(body), 0o600); err != nil {
		t.Fatalf("write requirements.txt: %v", err)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-pip-requirements",
		time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), snapshot, false, "")

	entities, bodyFacts := 0, 0
	var entityPayloads []string
	for _, envelope := range drainFactChannel(collected.Facts) {
		encoded, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatalf("marshal %s fact: %v", envelope.FactKind, err)
		}
		holdsSecret := strings.Contains(string(encoded), secret)
		switch envelope.FactKind {
		case "content_entity":
			entities++
			entityPayloads = append(entityPayloads, string(encoded))
			if holdsSecret {
				t.Errorf("content_entity fact holds the credential: %s", encoded)
			}
		case "content":
			if holdsSecret {
				bodyFacts++
			}
		default:
			if holdsSecret {
				t.Errorf("%s fact holds the credential: %s", envelope.FactKind, encoded)
			}
		}
	}
	if entities == 0 {
		t.Fatal("no content_entity facts emitted for requirements.txt; the test proved nothing")
	}
	if bodyFacts != 1 {
		t.Errorf("content facts holding the file body = %d, want 1: the body keeps the text until #8812 binds a policy", bodyFacts)
	}
	joined := strings.Join(entityPayloads, "\n")
	for _, want := range []string{"host.example", "git.example.com", "files.example.com", "privpkg", "privvcs", "privbranch", "plain"} {
		if !strings.Contains(joined, want) {
			t.Errorf("entity facts lost %q: host and package names must survive userinfo scrubbing", want)
		}
	}
	for _, want := range []string{"@v1.0.0", "@feature/foo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("entity facts lost ref %q: VCS refs and slash branches must survive userinfo scrubbing", want)
		}
	}
}
