// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocrdoc

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/imagepreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Engine recognizes text regions from a preflight-approved image.
type Engine interface {
	Recognize(context.Context, Image) (EngineResult, error)
}

// Options bounds OCR extraction work delegated by this package.
type Options struct {
	Preflight       imagepreflight.Options
	MaxSectionChars int
}

// Request describes one image artifact revision to extract as documentation.
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
}

// Image is the bounded image context passed to an OCR engine.
type Image struct {
	SourceName string
	Format     string
	Width      int
	Height     int
	FrameCount int
	FrameIndex int
	Body       []byte
}

// EngineResult is the source-neutral OCR output accepted by this package.
type EngineResult struct {
	EngineName    string
	EngineVersion string
	Language      string
	Regions       []Region
}

// Region describes one OCR text region.
type Region struct {
	RegionID   string
	Text       string
	Bounds     Bounds
	Confidence float64
}

// Bounds stores normalized OCR region coordinates.
type Bounds struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Result contains the document and section payloads plus ready-to-persist
// envelopes for one OCR extraction attempt.
type Result struct {
	Preflight imagepreflight.Result
	Document  facts.DocumentationDocumentPayload
	Sections  []facts.DocumentationSectionPayload
	Envelopes []facts.Envelope
}
