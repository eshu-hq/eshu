// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediadoc

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/mediapreflight"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Engine transcribes a preflight-approved local media artifact.
type Engine interface {
	Transcribe(context.Context, Media) (EngineResult, error)
}

// Options bounds media transcript extraction work delegated by this package.
type Options struct {
	Preflight       mediapreflight.Options
	MaxSectionChars int
}

// Request describes one media artifact revision to extract as documentation.
type Request struct {
	ScopeID        string
	GenerationID   string
	ObservedAt     time.Time
	SourceSystem   string
	SourceURI      string
	SourceRecordID string
	SourceName     string
	SourceID       string
	DocumentID     string
	ExternalID     string
	RevisionID     string
	CanonicalURI   string
	Title          string
	Body           []byte
	Engine         Engine
	Options        Options
	Entities       []doctruth.Entity
}

// Media is the bounded media context passed to a transcript engine.
type Media struct {
	SourceName       string
	Format           string
	DurationMillis   int64
	AudioStreamCount int
	Body             []byte
}

// EngineResult is the source-neutral transcript output accepted by this package.
type EngineResult struct {
	EngineName    string
	EngineVersion string
	Language      string
	Segments      []Segment
}

// Segment describes one transcript segment from a local engine result.
type Segment struct {
	SegmentID    string
	Text         string
	StartMillis  int64
	EndMillis    int64
	Confidence   float64
	SpeakerLabel string
	MentionHints []doctruth.MentionHint
}

// Result contains document and section payloads plus ready-to-persist envelopes
// for one media transcript extraction attempt.
type Result struct {
	Preflight mediapreflight.Result
	Document  facts.DocumentationDocumentPayload
	Sections  []facts.DocumentationSectionPayload
	Envelopes []facts.Envelope
}
