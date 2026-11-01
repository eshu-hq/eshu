// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"context"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/preflight/media"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

// ErrNoSpeech is the sentinel an Engine wraps when it ran successfully but
// heard no speech in the media. Extract records it as a document fact with
// transcript_status=no_speech and the transcript_no_speech warning class
// instead of failing the extraction, so the outcome stays visible through
// fact readback.
var ErrNoSpeech = errors.New("transcript engine heard no speech")

// Engine transcribes a preflight-approved local media artifact.
type Engine interface {
	Transcribe(context.Context, Media) (EngineResult, error)
}

// Options bounds media transcript extraction work delegated by this package.
type Options struct {
	Preflight       media.Options
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
	// ModelName is the model file base name a model-backed engine loaded,
	// recorded as transcript_model section metadata when non-empty.
	ModelName string
	// ModelDigest is the pinned "sha256:<hex>" digest of that model file,
	// recorded as transcript_model_digest section metadata when non-empty.
	ModelDigest string
	Language    string
	Segments    []Segment
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
	Preflight media.Result
	Document  docs.DocumentPayload
	Sections  []docs.SectionPayload
	Envelopes []facts.Envelope
}
