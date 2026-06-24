// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package diagrampreflight

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// FormatSVG identifies an SVG diagram.
	FormatSVG = "svg"
	// FormatDrawIO identifies a draw.io XML diagram.
	FormatDrawIO = "drawio"
	// FormatExcalidraw identifies an Excalidraw JSON diagram.
	FormatExcalidraw = "excalidraw"
	// FormatMermaid identifies a Mermaid diagram source.
	FormatMermaid = "mermaid"
	// FormatPlantUML identifies a PlantUML diagram source.
	FormatPlantUML = "plantuml"
	// FormatD2 identifies a D2 diagram source.
	FormatD2 = "d2"
)

const (
	defaultMaxSourceBytes = int64(10 << 20)
	defaultMaxElements    = 10000
	defaultMaxDepth       = 64
	maxScannerTokenBytes  = 1024 * 1024
)

// WarningClass is a stable, low-cardinality diagram preflight failure class.
type WarningClass string

const (
	// WarningUnsupportedFormat marks diagram formats outside this preflight.
	WarningUnsupportedFormat WarningClass = "unsupported_format"
	// WarningMalformedXML marks malformed SVG or XML-backed diagram input.
	WarningMalformedXML WarningClass = "malformed_xml"
	// WarningMalformedJSON marks malformed JSON-backed diagram input.
	WarningMalformedJSON WarningClass = "malformed_json"
	// WarningResourceLimitExceeded marks source, element-count, or depth limits.
	WarningResourceLimitExceeded WarningClass = "resource_limit_exceeded"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout WarningClass = "timeout"
	// WarningUnsupportedRemoteInclude marks include or external-entity directives.
	WarningUnsupportedRemoteInclude WarningClass = "unsupported_remote_include"
	// WarningUnsupportedActiveContent marks script, event-handler, or JavaScript content.
	WarningUnsupportedActiveContent WarningClass = "unsupported_active_content"
	// WarningExternalReferenceSkipped marks external references that cannot be followed.
	WarningExternalReferenceSkipped WarningClass = "external_reference_skipped"
	// WarningSensitiveValueRedacted marks sensitive-looking values detected in preflight.
	WarningSensitiveValueRedacted WarningClass = "sensitive_value_redacted"
)

// Options bounds diagram preflight work.
type Options struct {
	MaxSourceBytes  int64
	MaxElements     int
	MaxXMLJSONDepth int
}

// Warning records one bounded diagram preflight failure class.
type Warning struct {
	Class WarningClass `json:"class"`
	Count int          `json:"count"`
}

// Result summarizes metadata-only diagram preflight.
type Result struct {
	Format                 string    `json:"format"`
	Safe                   bool      `json:"safe"`
	Warnings               []Warning `json:"warnings,omitempty"`
	SourceBytes            int64     `json:"source_bytes"`
	ElementCount           int       `json:"element_count"`
	MaxObservedDepth       int       `json:"max_observed_depth"`
	ExternalReferenceCount int       `json:"external_reference_count"`
	IncludeCount           int       `json:"include_count"`
	ActiveContentCount     int       `json:"active_content_count"`
	SensitiveValueCount    int       `json:"sensitive_value_count"`
}

type recorder struct {
	result              *Result
	seen                map[WarningClass]int
	elementLimitWarning bool
	depthLimitWarning   bool
}

// Preflight classifies a diagram source without extracting labels or diagram text.
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
	if size < 0 || size > opts.MaxSourceBytes {
		rec.warn(WarningResourceLimitExceeded)
		return rec.finalize(), nil
	}
	body, ok := readBounded(reader, size, opts.MaxSourceBytes, &rec)
	if !ok {
		return rec.finalize(), nil
	}

	switch result.Format {
	case FormatSVG, FormatDrawIO:
		preflightXML(ctx, body, opts, &rec)
	case FormatExcalidraw:
		preflightJSON(ctx, body, opts, &rec)
	case FormatMermaid, FormatPlantUML, FormatD2:
		preflightText(ctx, body, opts, &rec)
	default:
		rec.warn(WarningUnsupportedFormat)
	}
	return rec.finalize(), nil
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	if options.MaxElements <= 0 {
		options.MaxElements = defaultMaxElements
	}
	if options.MaxXMLJSONDepth <= 0 {
		options.MaxXMLJSONDepth = defaultMaxDepth
	}
	return options
}

func readBounded(reader io.ReaderAt, size, maxBytes int64, rec *recorder) ([]byte, bool) {
	if size > maxBytes {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	buffer := make([]byte, size)
	n, err := reader.ReadAt(buffer, 0)
	if err != nil && (err != io.EOF || n != len(buffer)) {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	return buffer, true
}

func preflightXML(ctx context.Context, body []byte, options Options, rec *recorder) {
	rec.scanStructuredBody(string(body))
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		if err := ctx.Err(); err != nil {
			rec.warn(WarningTimeout)
			return
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			rec.warn(WarningMalformedXML)
			return
		}
		switch typed := token.(type) {
		case xml.StartElement:
			rec.observeElement(options)
			depth++
			rec.observeDepth(depth, options)
			rec.classifyXMLStart(typed)
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		}
	}
}

func preflightJSON(ctx context.Context, body []byte, options Options, rec *recorder) {
	if !json.Valid(body) {
		rec.warn(WarningMalformedJSON)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		if err := ctx.Err(); err != nil {
			rec.warn(WarningTimeout)
			return
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			rec.warn(WarningMalformedJSON)
			return
		}
		switch typed := token.(type) {
		case json.Delim:
			if typed == '{' || typed == '[' {
				rec.observeElement(options)
				depth++
				rec.observeDepth(depth, options)
				continue
			}
			if depth > 0 {
				depth--
			}
		case string:
			rec.scanText(typed)
		}
	}
}

func preflightText(ctx context.Context, body []byte, options Options, rec *recorder) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), maxScannerTokenBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			rec.warn(WarningTimeout)
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		rec.observeElement(options)
		rec.scanText(line)
	}
	if err := scanner.Err(); err != nil {
		rec.warn(WarningResourceLimitExceeded)
	}
}

func (r *recorder) observeElement(options Options) {
	r.result.ElementCount++
	if r.result.ElementCount > options.MaxElements && !r.elementLimitWarning {
		r.warn(WarningResourceLimitExceeded)
		r.elementLimitWarning = true
	}
}

func (r *recorder) observeDepth(depth int, options Options) {
	if depth > r.result.MaxObservedDepth {
		r.result.MaxObservedDepth = depth
	}
	if depth > options.MaxXMLJSONDepth && !r.depthLimitWarning {
		r.warn(WarningResourceLimitExceeded)
		r.depthLimitWarning = true
	}
}

func (r *recorder) classifyXMLStart(start xml.StartElement) {
	if strings.EqualFold(start.Name.Local, "script") || strings.EqualFold(start.Name.Local, "foreignObject") {
		r.activeContent()
	}
	for _, attr := range start.Attr {
		if attr.Name.Space == "xmlns" || strings.EqualFold(attr.Name.Local, "xmlns") {
			continue
		}
		local := strings.ToLower(attr.Name.Local)
		value := strings.TrimSpace(attr.Value)
		switch {
		case strings.HasPrefix(local, "on"):
			r.activeContent()
		case strings.EqualFold(local, "href") || strings.EqualFold(local, "src"):
			r.classifyReference(value)
		case strings.EqualFold(local, "include") || strings.EqualFold(local, "data"):
			if hasExternalReference(value) {
				r.externalReference()
			}
		default:
			r.scanText(value)
		}
	}
}

func (r *recorder) scanStructuredBody(text string) {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "<!entity") || strings.Contains(lower, "<!doctype") {
		r.includeReference()
	}
	if hasSensitiveMarker(lower) {
		r.result.SensitiveValueCount++
		r.warn(WarningSensitiveValueRedacted)
	}
}

func (r *recorder) scanText(text string) {
	lower := strings.ToLower(text)
	for _, marker := range []string{"!includeurl", "!include ", "!include\t", "include::", "@import "} {
		if strings.Contains(lower, marker) {
			r.includeReference()
			break
		}
	}
	if strings.Contains(lower, "javascript:") || strings.Contains(lower, "<script") || strings.Contains(lower, "onload=") {
		r.activeContent()
	}
	if hasExternalReference(lower) {
		r.externalReference()
	}
	if hasSensitiveMarker(lower) {
		r.result.SensitiveValueCount++
		r.warn(WarningSensitiveValueRedacted)
	}
}

func (r *recorder) classifyReference(value string) {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "javascript:") {
		r.activeContent()
		return
	}
	if hasExternalReference(lower) {
		r.externalReference()
	}
}

func (r *recorder) includeReference() {
	r.result.IncludeCount++
	r.warn(WarningUnsupportedRemoteInclude)
}

func (r *recorder) activeContent() {
	r.result.ActiveContentCount++
	r.warn(WarningUnsupportedActiveContent)
}

func (r *recorder) externalReference() {
	r.result.ExternalReferenceCount++
	r.warn(WarningExternalReferenceSkipped)
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
	switch strings.ToLower(filepath.Ext(sourceName)) {
	case ".svg":
		return FormatSVG
	case ".drawio":
		return FormatDrawIO
	case ".excalidraw":
		return FormatExcalidraw
	case ".mmd", ".mermaid":
		return FormatMermaid
	case ".puml", ".plantuml":
		return FormatPlantUML
	case ".d2":
		return FormatD2
	default:
		return ""
	}
}

func hasExternalReference(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "https://") ||
		strings.Contains(lower, "http://") ||
		strings.Contains(lower, "ftp://") ||
		strings.Contains(lower, "file://") ||
		strings.Contains(lower, "//")
}

func hasSensitiveMarker(text string) bool {
	for _, marker := range []string{
		"credential_marker",
		"api_key",
		"access_token",
		"auth_token",
		"password=",
		"passwd=",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
