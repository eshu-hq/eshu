// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pdfpreflight

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// FormatPDF identifies a PDF document.
	FormatPDF = "pdf"
)

const (
	defaultMaxSourceBytes = int64(50 << 20)
	maxReadBytes          = int64(50 << 20)
)

// WarningClass is a stable, low-cardinality PDF preflight failure class.
type WarningClass string

const (
	// WarningUnsupportedFormat marks document formats outside this preflight.
	WarningUnsupportedFormat WarningClass = "unsupported_format"
	// WarningMalformedPDF marks files that do not look like bounded PDFs.
	WarningMalformedPDF WarningClass = "malformed_pdf"
	// WarningUnsupportedEncrypted marks encrypted PDFs.
	WarningUnsupportedEncrypted WarningClass = "unsupported_encrypted"
	// WarningUnsupportedScannedPDF marks image-only PDFs detected without text evidence.
	WarningUnsupportedScannedPDF WarningClass = "unsupported_scanned_pdf"
	// WarningUnsupportedActiveContent marks JavaScript, launch, action content, or embedded files.
	WarningUnsupportedActiveContent WarningClass = "unsupported_active_content"
	// WarningResourceLimitExceeded marks source-byte or scan-budget limits.
	WarningResourceLimitExceeded WarningClass = "resource_limit_exceeded"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout WarningClass = "timeout"
	// WarningExternalReferenceSkipped marks external references that cannot be followed.
	WarningExternalReferenceSkipped WarningClass = "external_reference_skipped"
	// WarningAnnotationTextSkipped marks annotation payloads skipped by preflight.
	WarningAnnotationTextSkipped WarningClass = "annotation_text_skipped"
	// WarningMetadataRedacted marks source metadata fields that must not be persisted.
	WarningMetadataRedacted WarningClass = "metadata_redacted"
	// WarningPartialExtraction marks missing or incomplete PDF trailer markers.
	WarningPartialExtraction WarningClass = "partial_extraction"
)

// Options bounds PDF preflight work.
type Options struct {
	MaxSourceBytes int64
}

// Warning records one bounded PDF preflight failure class.
type Warning struct {
	Class WarningClass `json:"class"`
	Count int          `json:"count"`
}

// Result summarizes metadata-only PDF preflight.
type Result struct {
	Format                 string    `json:"format"`
	Safe                   bool      `json:"safe"`
	Warnings               []Warning `json:"warnings,omitempty"`
	SourceBytes            int64     `json:"source_bytes"`
	ObjectMarkerCount      int       `json:"object_marker_count"`
	PageMarkerCount        int       `json:"page_marker_count"`
	ExternalReferenceCount int       `json:"external_reference_count"`
	ActiveContentCount     int       `json:"active_content_count"`
	EmbeddedFileCount      int       `json:"embedded_file_count"`
	AnnotationCount        int       `json:"annotation_count"`
	MetadataRedactionCount int       `json:"metadata_redaction_count"`
	ImageObjectCount       int       `json:"image_object_count"`
}

type recorder struct {
	result *Result
	seen   map[WarningClass]int
}

// Preflight classifies a PDF source without extracting page text or metadata.
func Preflight(ctx context.Context, sourceName string, reader io.ReaderAt, size int64, options Options) (Result, error) {
	if reader == nil {
		return Result{}, fmt.Errorf("reader must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := normalizeOptions(options)
	result := Result{
		Format:      formatForSource(sourceName),
		Safe:        true,
		SourceBytes: size,
	}
	rec := recorder{result: &result, seen: map[WarningClass]int{}}

	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return rec.finalize(), err
	}
	if result.Format == "" {
		rec.warn(WarningUnsupportedFormat)
		return rec.finalize(), nil
	}
	if size < 0 || size > opts.MaxSourceBytes || size > maxReadBytes {
		rec.warn(WarningResourceLimitExceeded)
		return rec.finalize(), nil
	}
	body, ok := readExact(reader, size, &rec)
	if !ok {
		return rec.finalize(), nil
	}
	rec.classifyPDF(ctx, body)
	return rec.finalize(), nil
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	return options
}

func readExact(reader io.ReaderAt, size int64, rec *recorder) ([]byte, bool) {
	body := make([]byte, size)
	n, err := reader.ReadAt(body, 0)
	if err != nil && (err != io.EOF || n != len(body)) {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	return body, true
}

func (r *recorder) classifyPDF(ctx context.Context, body []byte) {
	if err := ctx.Err(); err != nil {
		r.warn(WarningTimeout)
		return
	}
	trimmed := bytes.TrimSpace(body)
	if !bytes.HasPrefix(trimmed, []byte("%PDF-")) {
		r.warn(WarningMalformedPDF)
		return
	}
	if !bytes.Contains(trimmed, []byte("%%EOF")) {
		r.warn(WarningPartialExtraction)
	}
	lower := strings.ToLower(string(body))
	r.result.ObjectMarkerCount = strings.Count(lower, " obj")
	r.result.PageMarkerCount = strings.Count(lower, "/type /page")
	r.result.ImageObjectCount = strings.Count(lower, "/subtype /image")

	if strings.Contains(lower, "/encrypt") {
		r.warn(WarningUnsupportedEncrypted)
	}
	if strings.Contains(lower, "/javascript") || strings.Contains(lower, "/js") ||
		strings.Contains(lower, "/openaction") || strings.Contains(lower, "/aa") ||
		strings.Contains(lower, "/launch") {
		r.result.ActiveContentCount++
		r.warn(WarningUnsupportedActiveContent)
	}
	if strings.Contains(lower, "/embeddedfile") || strings.Contains(lower, "/filespec") {
		r.result.EmbeddedFileCount++
		r.warn(WarningUnsupportedActiveContent)
	}
	if strings.Contains(lower, "/uri") || hasExternalReference(lower) {
		r.result.ExternalReferenceCount++
		r.warn(WarningExternalReferenceSkipped)
	}
	if strings.Contains(lower, "/annots") || strings.Contains(lower, "/annot") {
		r.result.AnnotationCount++
		r.warn(WarningAnnotationTextSkipped)
	}
	if strings.Contains(lower, "/title") || strings.Contains(lower, "/author") ||
		strings.Contains(lower, "/creator") || strings.Contains(lower, "/producer") ||
		strings.Contains(lower, "/subject") || strings.Contains(lower, "/keywords") {
		r.result.MetadataRedactionCount++
		r.warn(WarningMetadataRedacted)
	}
	if r.result.ImageObjectCount > 0 && r.result.PageMarkerCount == 0 {
		r.warn(WarningUnsupportedScannedPDF)
	}
}

func (r *recorder) warn(class WarningClass) {
	if count, ok := r.seen[class]; ok {
		r.seen[class] = count + 1
		for i := range r.result.Warnings {
			if r.result.Warnings[i].Class == class {
				r.result.Warnings[i].Count++
				break
			}
		}
	} else {
		r.seen[class] = 1
		r.result.Warnings = append(r.result.Warnings, Warning{Class: class, Count: 1})
	}
	r.result.Safe = false
}

func (r *recorder) finalize() Result {
	if len(r.result.Warnings) > 0 {
		r.result.Safe = false
		sort.Slice(r.result.Warnings, func(left, right int) bool {
			return r.result.Warnings[left].Class < r.result.Warnings[right].Class
		})
	}
	return *r.result
}

func formatForSource(sourceName string) string {
	if strings.EqualFold(filepath.Ext(sourceName), ".pdf") {
		return FormatPDF
	}
	return ""
}

func hasExternalReference(text string) bool {
	return strings.Contains(text, "https://") ||
		strings.Contains(text, "http://") ||
		strings.Contains(text, "ftp://") ||
		strings.Contains(text, "file://")
}
