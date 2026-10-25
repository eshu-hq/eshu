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

// TestPipConstraintsCredentialsStayOutOfEntityFacts drives the production path
// (NativeRepositorySnapshotter, then buildStreamingGeneration) over a
// constraints file whose lines carry credential-shaped text, and requires that
// no content_entity fact holds it (#8821). The entity source_cache is sliced
// from the file's line range, so a parser-level scrub alone is not enough: the
// raw line reached source_cache until the parser withheld it.
//
// The content fact keeps the whole file body. That is the content-store policy
// gap tracked by #8812, shared with requirements.txt, and this test pins it
// so the documented limit cannot drift: the body still holds the text.
func TestPipConstraintsCredentialsStayOutOfEntityFacts(t *testing.T) {
	t.Parallel()

	secret := "tok" + "en-" + "value-9f3a"
	body := strings.Join([]string{
		"--index-url https://ci-user:" + secret + "@index.example.com/simple",
		"privpkg @ https://ci-user:" + secret + "@host.example/p.whl",
		"pinned==1.0 # mirror https://ci-user:" + secret + "@mirror.example/simple",
		"marked==2.0 ; python_version > '3' and platform_node == 'https://ci-user:" + secret + "@m.example'",
		"plain==3.0",
		"",
	}, "\n")
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "constraints.txt"), []byte(body), 0o600); err != nil {
		t.Fatalf("write constraints.txt: %v", err)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-pip-constraints",
		time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), snapshot, false, "")

	entities, bodyFacts := 0, 0
	for _, envelope := range drainFactChannel(collected.Facts) {
		encoded, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatalf("marshal %s fact: %v", envelope.FactKind, err)
		}
		holdsSecret := strings.Contains(string(encoded), secret)
		switch envelope.FactKind {
		case "content_entity":
			entities++
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
		t.Fatal("no content_entity facts emitted for constraints.txt; the test proved nothing")
	}
	if bodyFacts != 1 {
		t.Errorf("content facts holding the file body = %d, want 1: the body keeps the text until #8812 binds a policy", bodyFacts)
	}
}
