// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"fmt"
	"os"
	"strings"
)

// EvidenceFormatMarkdown and EvidenceFormatJSON are the accepted artifact
// formats for the evidence report.
const (
	EvidenceFormatMarkdown = "md"
	EvidenceFormatJSON     = "json"
)

// NormalizeEvidenceFormat validates and canonicalizes an artifact format flag.
// It accepts "md"/"markdown" and "json" case-insensitively and returns an error
// listing the supported values otherwise.
func NormalizeEvidenceFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", EvidenceFormatMarkdown, "markdown":
		return EvidenceFormatMarkdown, nil
	case EvidenceFormatJSON:
		return EvidenceFormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported report format %q: supported formats are md, json", raw)
	}
}

// RenderEvidenceArtifact renders the report in the requested format and returns
// the bytes. The report is already redacted, so the bytes are safe to persist.
func RenderEvidenceArtifact(report EvidenceReport, format string) ([]byte, error) {
	normalized, err := NormalizeEvidenceFormat(format)
	if err != nil {
		return nil, err
	}
	if normalized == EvidenceFormatJSON {
		return renderEvidenceJSON(report)
	}
	markdown, err := renderEvidenceMarkdown(report)
	if err != nil {
		return nil, err
	}
	return []byte(markdown), nil
}

// WriteEvidenceArtifact writes the rendered artifact to path with owner-only
// permissions, since a support packet may still contain endpoint hostnames.
func WriteEvidenceArtifact(report EvidenceReport, format, path string) error {
	data, err := RenderEvidenceArtifact(report, format)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600) //nolint:wrapcheck // *fs.PathError already names the operation and the path; a wrap doubled both in operator stderr on a sibling extraction.
}
