// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opdigest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// artifactSchema is the operator_digest_artifact.v1 schema marker every
	// Artifact carries.
	artifactSchema = "operator_digest_artifact.v1"
	// artifactFormat is the artifact's on-disk encoding. It is the only
	// format this package writes.
	artifactFormat = "json"
	// artifactWriterCLI is the Artifact.WriterKind value the CLI writer
	// stamps; it distinguishes CLI-produced artifacts from a future
	// non-CLI writer.
	artifactWriterCLI = "cli"
	// redactionProfile is the share-safe redaction profile every Artifact
	// applies before it is written.
	redactionProfile = "share_safe_v1"
)

// Artifact is the operator_digest_artifact.v1 shareable handoff wrapper
// around one Digest.
type Artifact struct {
	Schema      string             `json:"schema"`
	Digest      Digest             `json:"digest"`
	Artifact    ArtifactMetadata   `json:"artifact"`
	Redaction   ArtifactRedaction  `json:"redaction"`
	SourceRefs  []SourceRef        `json:"source_refs"`
	Validation  ArtifactValidation `json:"validation"`
	Limitations []Limitation       `json:"limitations"`
}

// ArtifactMetadata identifies one Artifact and how it was produced.
type ArtifactMetadata struct {
	ID               string `json:"id"`
	WriterKind       string `json:"writer_kind"`
	Format           string `json:"format"`
	ValidationStatus string `json:"validation_status"`
}

// ArtifactRedaction records the redaction profile and rules an Artifact's
// writer applied.
type ArtifactRedaction struct {
	Profile        string   `json:"profile"`
	Version        string   `json:"version"`
	AppliedRules   []string `json:"applied_rules"`
	ReplacedFields []string `json:"replaced_fields"`
}

// ArtifactValidation is the aggregate pass/fail outcome of the checks
// BuildArtifact runs before returning an Artifact.
type ArtifactValidation struct {
	Status string          `json:"status"`
	Checks []ArtifactCheck `json:"checks"`
}

// ArtifactCheck is one named validation outcome within ArtifactValidation.
type ArtifactCheck struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`
}

// WriteArtifact builds the operator_digest_artifact.v1 wrapper around digest
// and writes it as indented JSON to path, mode 0600. It never reads a
// cobra flag or the process environment; the caller (the `eshu report`
// wrapper) resolves --artifact-out to path first.
func WriteArtifact(path string, digest Digest) error {
	artifact, err := BuildArtifact(digest)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("encode operator digest artifact: %w", err)
	}
	data = append(data, '\n')
	if err := writeArtifactFile(path, data); err != nil {
		return fmt.Errorf("write operator digest artifact: %w", err)
	}
	return nil
}

// writeArtifactFile returns the *fs.PathError from os/File unwrapped, on
// purpose. Those errors already render as "open <path>: <cause>", and
// WriteArtifact's caller adds the "write operator digest artifact: " prefix,
// so wrapping here would print the path twice in operator-facing stderr.
// The `eshu report --artifact-out` message text is a CLI contract; keep it
// byte-identical.
func writeArtifactFile(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- path is an operator-supplied CLI output path, not an HTTP request param
	if err != nil {
		return err //nolint:wrapcheck // *fs.PathError already names the path; see the comment above.
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	n, err := file.Write(data)
	if err != nil {
		return err //nolint:wrapcheck // *fs.PathError already names the path; see the comment above.
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return file.Chmod(0o600) //nolint:wrapcheck // *fs.PathError already names the path; see the comment above.
}

// BuildArtifact wraps digest in the operator_digest_artifact.v1 shape,
// computes its content-derived Artifact.ID, and validates the result
// against the artifact contract (schema markers, required fields, source
// ref completeness, redaction metadata) before returning it.
func BuildArtifact(digest Digest) (Artifact, error) {
	sourceRefs := dedupeSourceRefs(digest.SourceRefs)
	artifact := Artifact{
		Schema:     artifactSchema,
		Digest:     digest,
		SourceRefs: dedupeSourceRefs(append(sourceRefs, questionSourceRefs(digest.SuggestedQuestions)...)),
		Artifact: ArtifactMetadata{
			WriterKind:       artifactWriterCLI,
			Format:           artifactFormat,
			ValidationStatus: "passed",
		},
		Redaction: ArtifactRedaction{
			Profile: redactionProfile,
			Version: "1",
			AppliedRules: []string{
				"scope_share_safe_validation",
				"source_ref_deduplication",
				"no_transport_metadata",
			},
			ReplacedFields: []string{},
		},
		Validation: ArtifactValidation{
			Status: "passed",
			Checks: []ArtifactCheck{
				{ID: "schema", OK: true},
				{ID: "required_digest_fields", OK: true},
				{ID: "source_refs", OK: true},
				{ID: "question_evidence", OK: true},
				{ID: "redaction", OK: true},
			},
		},
		Limitations: digest.Limitations,
	}
	artifact.Artifact.ID = artifactID(artifact)
	if err := validateArtifact(artifact); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func validateArtifact(artifact Artifact) error {
	if artifact.Schema != artifactSchema {
		return fmt.Errorf("operator digest artifact schema = %q, want %q", artifact.Schema, artifactSchema)
	}
	if artifact.Digest.Schema != Schema {
		return fmt.Errorf("operator digest schema = %q, want %q", artifact.Digest.Schema, Schema)
	}
	if strings.TrimSpace(artifact.Digest.Scope.ID) == "" || strings.TrimSpace(artifact.Digest.Profile) == "" {
		return fmt.Errorf("operator digest artifact missing scope or profile")
	}
	if artifact.Digest.Truth.TruthClass == "" || artifact.Digest.Truth.Freshness == "" {
		return fmt.Errorf("operator digest artifact missing truth metadata")
	}
	if len(artifact.Digest.Sections) == 0 || len(artifact.Digest.Limitations) == 0 {
		return fmt.Errorf("operator digest artifact missing sections or limitations")
	}
	sourceIDs := make(map[string]struct{}, len(artifact.SourceRefs))
	for _, ref := range artifact.SourceRefs {
		if strings.TrimSpace(ref.ID) == "" {
			return fmt.Errorf("operator digest artifact has empty source ref")
		}
		sourceIDs[ref.ID] = struct{}{}
	}
	if len(sourceIDs) == 0 {
		return fmt.Errorf("operator digest artifact missing source refs")
	}
	for _, section := range artifact.Digest.Sections {
		if len(section.SourceRefs) == 0 && len(section.Limitations) == 0 {
			return fmt.Errorf("operator digest section %q has no source ref or limitation", section.ID)
		}
		for _, refID := range section.SourceRefs {
			if _, ok := sourceIDs[refID]; !ok {
				return fmt.Errorf("operator digest section %q references unknown source %q", section.ID, refID)
			}
		}
	}
	for _, question := range artifact.Digest.SuggestedQuestions {
		if question.SourceSignal == "" && len(question.EvidenceRefs) == 0 {
			return fmt.Errorf("operator digest question %q has no source signal or evidence ref", question.ID)
		}
		if strings.TrimSpace(question.Why) == "" {
			return fmt.Errorf("operator digest question %q has no why", question.ID)
		}
		if _, ok := sourceIDs[question.Target]; !ok {
			return fmt.Errorf("operator digest question %q references unknown target %q", question.ID, question.Target)
		}
	}
	if artifact.Redaction.Profile != redactionProfile || len(artifact.Redaction.AppliedRules) == 0 {
		return fmt.Errorf("operator digest artifact missing redaction metadata")
	}
	if artifact.Validation.Status != "passed" || artifact.Artifact.ValidationStatus != "passed" || artifact.Artifact.ID == "" {
		return fmt.Errorf("operator digest artifact validation did not pass")
	}
	return nil
}

func questionSourceRefs(questions []Question) []SourceRef {
	refs := make([]SourceRef, 0, len(questions))
	for _, question := range questions {
		target := strings.TrimSpace(question.Target)
		if target == "" {
			continue
		}
		refs = append(refs, SourceRef{
			ID:   target,
			Kind: targetKind(target),
			Name: target,
		})
	}
	return refs
}

func targetKind(target string) string {
	switch {
	case strings.HasPrefix(target, "mcp:"):
		return "mcp_tool"
	case strings.HasPrefix(target, "route:"):
		return "http_route"
	case strings.HasPrefix(target, "query-playbook:"):
		return "query_playbook"
	default:
		return "reference"
	}
}

func dedupeSourceRefs(refs []SourceRef) []SourceRef {
	seen := make(map[string]struct{}, len(refs))
	out := make([]SourceRef, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.ID) == "" {
			continue
		}
		if _, ok := seen[ref.ID]; ok {
			continue
		}
		seen[ref.ID] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func artifactID(artifact Artifact) string {
	var b strings.Builder
	b.WriteString(artifact.Schema)
	b.WriteString("|")
	b.WriteString(artifact.Digest.Schema)
	b.WriteString("|")
	b.WriteString(artifact.Digest.Scope.ID)
	b.WriteString("|")
	b.WriteString(artifact.Digest.Profile)
	b.WriteString("|")
	b.WriteString(artifact.Redaction.Profile)
	for _, ref := range artifact.SourceRefs {
		b.WriteString("|")
		b.WriteString(ref.ID)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "operator-digest-artifact:" + hex.EncodeToString(sum[:])
}
