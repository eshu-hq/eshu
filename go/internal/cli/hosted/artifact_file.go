// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"fmt"
	"os"
	"strings"
)

// artifactFormatMarkdown and artifactFormatJSON are the accepted artifact
// formats for the onboarding artifact written by --out. They mirror the
// evidence-report formats the CLI accepts elsewhere, so an operator learns one
// --format vocabulary; the duplication is deliberate, because the evidence
// report is a separate command family with its own lifecycle.
const (
	artifactFormatMarkdown = "md"
	artifactFormatJSON     = "json"
)

// normalizeArtifactFormat validates and canonicalizes an artifact format flag.
// It accepts "md"/"markdown" and "json" case-insensitively and returns an error
// listing the supported values otherwise.
func normalizeArtifactFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", artifactFormatMarkdown, "markdown":
		return artifactFormatMarkdown, nil
	case artifactFormatJSON:
		return artifactFormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported report format %q: supported formats are md, json", raw)
	}
}

// WriteArtifact renders the artifact in the requested format and
// writes it with owner-only permissions, since it still carries endpoint
// hostnames an operator may not want world-readable.
//
// The os.WriteFile error is returned unwrapped so the operator sees the same
// path-and-reason message the command printed before this package existed.
//
//nolint:wrapcheck // preserves the operator-visible write error verbatim.
func WriteArtifact(artifact Artifact, format, path string) error {
	normalized, err := normalizeArtifactFormat(format)
	if err != nil {
		return err
	}
	var data []byte
	if normalized == artifactFormatJSON {
		data, err = RenderArtifactJSON(artifact)
	} else {
		var markdown string
		markdown, err = RenderArtifactMarkdown(artifact)
		data = []byte(markdown)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Endpoint redaction is evidredact.Endpoint, the CLI-wide rule the evidence
// report family also uses: embedded userinfo, credential-named query values,
// and the whole fragment are removed while scheme, host, and path survive so
// the operator can still recognize the target. This package used to carry its
// own userinfo-only copy from before evidredact existed; the shared rule is
// strictly stronger, and one rule means the onboarding artifact cannot drift
// behind the evidence report's redaction again.
