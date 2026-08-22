// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitmodel

import (
	"crypto/sha1" // #nosec G505 -- non-cryptographic content digest for documentation file deduplication, not a security primitive
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// FactStreamWriter wraps a fact channel with an atomic counter. Every send
// through this writer increments the counter atomically so the stream
// produces an exact post-drain count without pre-reading file bodies.
type FactStreamWriter struct {
	ch    chan<- facts.Envelope
	count *atomic.Int64
	ref   string
}

// NewFactStreamWriter builds a writer over the fact channel that a collected
// generation streams through. It exists because the writer's fields stay
// unexported across the package boundary: Send increments count on every
// envelope, so a caller that could build the struct literally could also build
// one with a nil counter and panic on the first fact.
//
// A nil count is substituted rather than rejected, so the guard is real and not
// merely documentary. Every current caller passes a non-nil counter; a
// substituted one keeps Send total and loses only the caller's ability to read
// the tally back, which is what a nil counter already meant.
func NewFactStreamWriter(ch chan<- facts.Envelope, count *atomic.Int64, ref string) FactStreamWriter {
	if count == nil {
		count = new(atomic.Int64)
	}
	return FactStreamWriter{ch: ch, count: count, ref: ref}
}

func FactEnvelope(
	factKind string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	factKey string,
	payload map[string]any,
	sourceURI string,
) facts.Envelope {
	return facts.Envelope{
		FactID: facts.StableID(
			"GoGitCollectorFact",
			map[string]any{
				"fact_key":      factKey,
				"fact_kind":     factKind,
				"generation_id": generationID,
				"scope_id":      scopeID,
			},
		),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    factKey,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        factKey,
			SourceURI:      sourceURI,
			SourceRecordID: factKey,
		},
	}
}

func RepositoryRelativePath(repoPath string, filePath string) string {
	relativePath, err := filepath.Rel(repoPath, filePath)
	if err != nil {
		return filepath.Base(filePath)
	}
	return filepath.ToSlash(relativePath)
}

func PayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		return text
	}
	return ""
}

func PayloadPath(payload map[string]any, key string) string {
	value := PayloadString(payload, key)
	if value == "" {
		return ""
	}
	resolved, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return resolved
}

func FirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func DocumentationDigestForFile(filePath string) (string, bool) {
	file, err := os.Open(filePath) // #nosec G304 -- reads indexed repo documentation file at a path derived from the scan target, not user-supplied input
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	hash := sha1.New() // #nosec G401 -- non-cryptographic content digest for documentation file deduplication, not a security primitive
	if _, err := io.Copy(hash, file); err != nil {
		return "", false
	}
	return hex.EncodeToString(hash.Sum(nil)), true
}

func (w FactStreamWriter) Send(env facts.Envelope) {
	if w.ref != "" {
		if env.Payload == nil {
			env.Payload = map[string]any{}
		}
		env.Payload["ref"] = w.ref
	}
	w.count.Add(1)
	w.ch <- env
}
