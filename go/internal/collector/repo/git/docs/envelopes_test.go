// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	factsdocs "github.com/eshu-hq/eshu/go/internal/facts/docs"
)

func envelopeKinds(envelopes []facts.Envelope) map[string]int {
	kinds := map[string]int{}
	for _, envelope := range envelopes {
		kinds[envelope.FactKind]++
	}
	return kinds
}

func TestGitDocumentationEnvelopesForContentFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	observedAt := time.Now()
	body := []byte("# Hello\n\nSee [guide](docs/guide.md).\n")

	t.Run("unsupported path emits nothing", func(t *testing.T) {
		t.Parallel()

		envelopes, emitted := GitDocumentationEnvelopesForContentFile(
			ctx, t.TempDir(), testDocRepo(), "scope", "gen", observedAt, "main.go", "d", "c", body, true)
		if emitted || len(envelopes) != 0 {
			t.Fatalf("envelopes = (%d, %v), want (0, false)", len(envelopes), emitted)
		}
	})

	t.Run("markdown emits source document sections and links", func(t *testing.T) {
		t.Parallel()

		envelopes, emitted := GitDocumentationEnvelopesForContentFile(
			ctx, t.TempDir(), testDocRepo(), "scope", "gen", observedAt, "README.md", "d", "c", body, true)
		if !emitted {
			t.Fatal("sourceEmitted = false, want true")
		}
		kinds := envelopeKinds(envelopes)
		if kinds[factsdocs.SourceFactKind] != 1 {
			t.Fatalf("kinds = %v, want one source envelope", kinds)
		}
		if kinds[factsdocs.DocumentFactKind] != 1 {
			t.Fatalf("kinds = %v, want one document envelope", kinds)
		}
		if kinds[factsdocs.SectionFactKind] == 0 {
			t.Fatalf("kinds = %v, want section envelopes", kinds)
		}
		if kinds[factsdocs.LinkFactKind] != 1 {
			t.Fatalf("kinds = %v, want one link envelope", kinds)
		}
	})

	t.Run("without source flag no source envelope leaves", func(t *testing.T) {
		t.Parallel()

		envelopes, emitted := GitDocumentationEnvelopesForContentFile(
			ctx, t.TempDir(), testDocRepo(), "scope", "gen", observedAt, "notes.md", "d", "c", body, false)
		if emitted {
			t.Fatal("sourceEmitted = true, want false")
		}
		if kinds := envelopeKinds(envelopes); kinds[factsdocs.SourceFactKind] != 0 {
			t.Fatalf("kinds = %v, want no source envelope", kinds)
		}
	})
}

func TestEmitGitDocumentationFactsForContentFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	observedAt := time.Now()
	body := []byte("# Hello\n\nbody\n")

	envelopes, _ := GitDocumentationEnvelopesForContentFile(
		ctx, t.TempDir(), testDocRepo(), "scope", "gen", observedAt, "notes.md", "d", "c", body, true)
	if len(envelopes) == 0 {
		t.Fatal("no envelopes to emit")
	}

	ch := make(chan facts.Envelope, len(envelopes))
	var count atomic.Int64
	writer := model.NewFactStreamWriter(ch, &count, "test")
	if !EmitGitDocumentationFactsForContentFile(
		ctx, writer, t.TempDir(), testDocRepo(), "scope", "gen", observedAt, "notes.md", "d", "c", body, true) {
		t.Fatal("EmitGitDocumentationFactsForContentFile() = false, want true")
	}
	if int(count.Load()) != len(envelopes) {
		t.Fatalf("sent = %d, want %d", count.Load(), len(envelopes))
	}
	for range envelopes {
		env := <-ch
		if env.Payload["ref"] != "test" {
			t.Fatalf("payload = %v, want writer ref stamp", env.Payload)
		}
	}
}
