package ooxmlpreflight

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// FormatDOCX identifies an OOXML word-processing package.
	FormatDOCX = "docx"
	// FormatXLSX identifies an OOXML workbook package.
	FormatXLSX = "xlsx"
	// FormatPPTX identifies an OOXML presentation package.
	FormatPPTX = "pptx"

	// WarningUnsupportedFormat marks a file extension outside this preflight.
	WarningUnsupportedFormat = "unsupported_format"
	// WarningUnsupportedMacroEnabled marks macro-enabled Office packages or macro parts.
	WarningUnsupportedMacroEnabled = "unsupported_macro_enabled"
	// WarningMalformedContainer marks ZIP container parse failures.
	WarningMalformedContainer = "malformed_container"
	// WarningMalformedXML marks malformed package metadata or relationship XML.
	WarningMalformedXML = "malformed_xml"
	// WarningResourceLimitExceeded marks source, entry-count, expanded-byte, or XML limits.
	WarningResourceLimitExceeded = "resource_limit_exceeded"
	// WarningCompressionRatioExceeded marks package parts over the compression-ratio cap.
	WarningCompressionRatioExceeded = "compression_ratio_exceeded"
	// WarningArchivePathEscape marks absolute, parent-traversing, or non-local part names.
	WarningArchivePathEscape = "archive_path_escape"
	// WarningExternalRelationship marks relationships that point outside the package.
	WarningExternalRelationship = "external_relationship"
	// WarningActiveContent marks ActiveX or executable macro-like package parts.
	WarningActiveContent = "active_content_present"
	// WarningEmbeddedObject marks embedded object package parts.
	WarningEmbeddedObject = "embedded_object_present"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout = "timeout"
)

const (
	defaultMaxSourceBytes         = int64(50 << 20)
	defaultMaxExpandedBytes       = uint64(128 << 20)
	defaultMaxEntries             = 2000
	defaultMaxCompressionRatio    = 100
	defaultMaxXMLBytes            = uint64(1 << 20)
	defaultMaxXMLDepth            = 64
	ratioCheckMinUncompressedSize = uint64(1024)
)

// Options bounds OOXML package preflight.
type Options struct {
	MaxSourceBytes      int64
	MaxExpandedBytes    uint64
	MaxEntries          int
	MaxCompressionRatio float64
	MaxXMLBytes         uint64
	MaxXMLDepth         int
}

// Warning records one bounded preflight failure class.
type Warning struct {
	Class string `json:"class"`
	Count int    `json:"count"`
}

// Result summarizes metadata-only OOXML package preflight.
type Result struct {
	Format                    string    `json:"format"`
	Safe                      bool      `json:"safe"`
	Warnings                  []Warning `json:"warnings,omitempty"`
	PartCount                 int       `json:"part_count"`
	SourceBytes               int64     `json:"source_bytes"`
	ExpandedBytes             uint64    `json:"expanded_bytes"`
	RelationshipPartCount     int       `json:"relationship_part_count"`
	ExternalRelationshipCount int       `json:"external_relationship_count"`
	ActiveContentCount        int       `json:"active_content_count"`
	EmbeddedObjectCount       int       `json:"embedded_object_count"`
}

type recorder struct {
	result                *Result
	seen                  map[string]int
	expandedBytesExceeded bool
}

// Preflight classifies an OOXML package without extracting document content.
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
	rec := recorder{result: &result, seen: map[string]int{}}

	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return rec.finalize(), nil
	}
	if result.Format == "" {
		rec.warn(WarningUnsupportedFormat)
		return rec.finalize(), nil
	}
	if isMacroEnabledSource(sourceName) {
		rec.warn(WarningUnsupportedMacroEnabled)
		return rec.finalize(), nil
	}
	if size < 0 {
		rec.warn(WarningMalformedContainer)
		return rec.finalize(), nil
	}
	if size > opts.MaxSourceBytes {
		rec.warn(WarningResourceLimitExceeded)
		return rec.finalize(), nil
	}

	zr, err := zip.NewReader(reader, size)
	if err != nil {
		if !errors.Is(err, zip.ErrInsecurePath) || zr == nil {
			rec.warn(WarningMalformedContainer)
			return rec.finalize(), nil
		}
		rec.warn(WarningArchivePathEscape)
	}
	if len(zr.File) > opts.MaxEntries {
		rec.warn(WarningResourceLimitExceeded)
	}

	for _, file := range zr.File {
		if err := ctx.Err(); err != nil {
			rec.warn(WarningTimeout)
			return rec.finalize(), nil
		}
		result.PartCount++
		name := file.Name
		if unsafeZipPartName(name) {
			rec.warn(WarningArchivePathEscape)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		result.ExpandedBytes += file.UncompressedSize64
		if result.ExpandedBytes > opts.MaxExpandedBytes && !rec.expandedBytesExceeded {
			rec.warn(WarningResourceLimitExceeded)
			rec.expandedBytesExceeded = true
		}
		if compressionRatioExceeded(file, opts.MaxCompressionRatio) {
			rec.warn(WarningCompressionRatioExceeded)
		}
		rec.classifyPartName(name)
		if shouldParseXMLMetadata(name) {
			if parseXMLMetadata(ctx, file, opts, &rec) {
				if isRelationshipPart(name) {
					result.RelationshipPartCount++
				}
			}
		}
	}
	return rec.finalize(), nil
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	if options.MaxExpandedBytes == 0 {
		options.MaxExpandedBytes = defaultMaxExpandedBytes
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaultMaxEntries
	}
	if options.MaxCompressionRatio <= 0 {
		options.MaxCompressionRatio = defaultMaxCompressionRatio
	}
	if options.MaxXMLBytes == 0 {
		options.MaxXMLBytes = defaultMaxXMLBytes
	}
	if options.MaxXMLDepth <= 0 {
		options.MaxXMLDepth = defaultMaxXMLDepth
	}
	return options
}

func (r *recorder) warn(class string) {
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
	case ".docx", ".docm":
		return FormatDOCX
	case ".xlsx", ".xlsm":
		return FormatXLSX
	case ".pptx", ".pptm":
		return FormatPPTX
	default:
		return ""
	}
}

func isMacroEnabledSource(sourceName string) bool {
	switch strings.ToLower(filepath.Ext(sourceName)) {
	case ".docm", ".xlsm", ".pptm":
		return true
	default:
		return false
	}
}

func unsafeZipPartName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") {
		return true
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || hasWindowsDrivePrefix(trimmed) {
		return true
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	cleaned := path.Clean(trimmed)
	return cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func hasWindowsDrivePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	first := name[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func compressionRatioExceeded(file *zip.File, maxRatio float64) bool {
	if file.UncompressedSize64 < ratioCheckMinUncompressedSize {
		return false
	}
	if file.CompressedSize64 == 0 {
		return file.UncompressedSize64 > 0
	}
	return float64(file.UncompressedSize64)/float64(file.CompressedSize64) > maxRatio
}

func (r *recorder) classifyPartName(name string) {
	lower := strings.ToLower(strings.TrimPrefix(name, "/"))
	switch {
	case strings.Contains(lower, "vbaproject.bin"):
		r.warn(WarningUnsupportedMacroEnabled)
		r.warn(WarningActiveContent)
		r.result.ActiveContentCount++
	case strings.Contains(lower, "/activex/") || strings.HasPrefix(lower, "activex/"):
		r.warn(WarningActiveContent)
		r.result.ActiveContentCount++
	case strings.Contains(lower, "/embeddings/") || strings.HasPrefix(lower, "embeddings/"):
		r.warn(WarningEmbeddedObject)
		r.result.EmbeddedObjectCount++
	}
}

func shouldParseXMLMetadata(name string) bool {
	lower := strings.ToLower(name)
	return lower == "[content_types].xml" || isRelationshipPart(lower)
}

func isRelationshipPart(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".rels")
}

func parseXMLMetadata(ctx context.Context, file *zip.File, options Options, rec *recorder) bool {
	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return false
	}
	if file.UncompressedSize64 > options.MaxXMLBytes {
		rec.warn(WarningResourceLimitExceeded)
		return false
	}
	body, ok := readZipPart(file, options.MaxXMLBytes, rec)
	if !ok {
		return false
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return true
		}
		if err != nil {
			rec.warn(WarningMalformedXML)
			return false
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if depth > options.MaxXMLDepth {
				rec.warn(WarningResourceLimitExceeded)
				return false
			}
			rec.classifyXMLElement(typed)
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
		}
	}
}

func readZipPart(file *zip.File, maxBytes uint64, rec *recorder) ([]byte, bool) {
	reader, err := file.Open()
	if err != nil {
		rec.warn(WarningMalformedContainer)
		return nil, false
	}
	defer func() {
		if err := reader.Close(); err != nil {
			rec.warn(WarningMalformedContainer)
		}
	}()

	limited := io.LimitReader(reader, int64(maxBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		rec.warn(WarningMalformedContainer)
		return nil, false
	}
	if uint64(len(body)) > maxBytes {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	return body, true
}

func (r *recorder) classifyXMLElement(start xml.StartElement) {
	switch strings.ToLower(start.Name.Local) {
	case "relationship":
		if hasAttrValue(start, "TargetMode", "External") {
			r.warn(WarningExternalRelationship)
			r.result.ExternalRelationshipCount++
		}
	case "override", "default":
		contentType := attrValue(start, "ContentType")
		r.classifyContentType(contentType)
	}
}

func (r *recorder) classifyContentType(contentType string) {
	lower := strings.ToLower(contentType)
	switch {
	case strings.Contains(lower, "macroenabled") || strings.Contains(lower, "vba"):
		r.warn(WarningUnsupportedMacroEnabled)
	case strings.Contains(lower, "activex"):
		r.warn(WarningActiveContent)
		r.result.ActiveContentCount++
	case strings.Contains(lower, "oleobject") || strings.Contains(lower, "embedding"):
		r.warn(WarningEmbeddedObject)
		r.result.EmbeddedObjectCount++
	}
}

func attrValue(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}

func hasAttrValue(start xml.StartElement, name, value string) bool {
	return strings.EqualFold(attrValue(start, name), value)
}
