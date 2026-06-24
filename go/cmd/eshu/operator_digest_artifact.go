// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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
	operatorDigestArtifactSchema    = "operator_digest_artifact.v1"
	operatorDigestArtifactFormat    = "json"
	operatorDigestArtifactWriterCLI = "cli"
	operatorDigestRedactionProfile  = "share_safe_v1"
)

type operatorDigestArtifact struct {
	Schema      string                           `json:"schema"`
	Digest      operatorDigest                   `json:"digest"`
	Artifact    operatorDigestArtifactMetadata   `json:"artifact"`
	Redaction   operatorDigestArtifactRedaction  `json:"redaction"`
	SourceRefs  []operatorDigestSourceRef        `json:"source_refs"`
	Validation  operatorDigestArtifactValidation `json:"validation"`
	Limitations []operatorDigestLimitation       `json:"limitations"`
}

type operatorDigestArtifactMetadata struct {
	ID               string `json:"id"`
	WriterKind       string `json:"writer_kind"`
	Format           string `json:"format"`
	ValidationStatus string `json:"validation_status"`
}

type operatorDigestArtifactRedaction struct {
	Profile        string   `json:"profile"`
	Version        string   `json:"version"`
	AppliedRules   []string `json:"applied_rules"`
	ReplacedFields []string `json:"replaced_fields"`
}

type operatorDigestArtifactValidation struct {
	Status string                        `json:"status"`
	Checks []operatorDigestArtifactCheck `json:"checks"`
}

type operatorDigestArtifactCheck struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`
}

func writeOperatorDigestArtifact(path string, digest operatorDigest) error {
	artifact, err := buildOperatorDigestArtifact(digest)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("encode operator digest artifact: %w", err)
	}
	data = append(data, '\n')
	if err := writeOperatorDigestArtifactFile(path, data); err != nil {
		return fmt.Errorf("write operator digest artifact: %w", err)
	}
	return nil
}

func writeOperatorDigestArtifactFile(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	n, err := file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return file.Chmod(0o600)
}

func buildOperatorDigestArtifact(digest operatorDigest) (operatorDigestArtifact, error) {
	sourceRefs := dedupeOperatorDigestSourceRefs(digest.SourceRefs)
	artifact := operatorDigestArtifact{
		Schema:     operatorDigestArtifactSchema,
		Digest:     digest,
		SourceRefs: dedupeOperatorDigestSourceRefs(append(sourceRefs, operatorDigestQuestionSourceRefs(digest.SuggestedQuestions)...)),
		Artifact: operatorDigestArtifactMetadata{
			WriterKind:       operatorDigestArtifactWriterCLI,
			Format:           operatorDigestArtifactFormat,
			ValidationStatus: "passed",
		},
		Redaction: operatorDigestArtifactRedaction{
			Profile: operatorDigestRedactionProfile,
			Version: "1",
			AppliedRules: []string{
				"scope_share_safe_validation",
				"source_ref_deduplication",
				"no_transport_metadata",
			},
			ReplacedFields: []string{},
		},
		Validation: operatorDigestArtifactValidation{
			Status: "passed",
			Checks: []operatorDigestArtifactCheck{
				{ID: "schema", OK: true},
				{ID: "required_digest_fields", OK: true},
				{ID: "source_refs", OK: true},
				{ID: "question_evidence", OK: true},
				{ID: "redaction", OK: true},
			},
		},
		Limitations: digest.Limitations,
	}
	artifact.Artifact.ID = operatorDigestArtifactID(artifact)
	if err := validateOperatorDigestArtifact(artifact); err != nil {
		return operatorDigestArtifact{}, err
	}
	return artifact, nil
}

func validateOperatorDigestArtifact(artifact operatorDigestArtifact) error {
	if artifact.Schema != operatorDigestArtifactSchema {
		return fmt.Errorf("operator digest artifact schema = %q, want %q", artifact.Schema, operatorDigestArtifactSchema)
	}
	if artifact.Digest.Schema != operatorDigestSchema {
		return fmt.Errorf("operator digest schema = %q, want %q", artifact.Digest.Schema, operatorDigestSchema)
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
	if artifact.Redaction.Profile != operatorDigestRedactionProfile || len(artifact.Redaction.AppliedRules) == 0 {
		return fmt.Errorf("operator digest artifact missing redaction metadata")
	}
	if artifact.Validation.Status != "passed" || artifact.Artifact.ValidationStatus != "passed" || artifact.Artifact.ID == "" {
		return fmt.Errorf("operator digest artifact validation did not pass")
	}
	return nil
}

func operatorDigestQuestionSourceRefs(questions []operatorDigestQuestion) []operatorDigestSourceRef {
	refs := make([]operatorDigestSourceRef, 0, len(questions))
	for _, question := range questions {
		target := strings.TrimSpace(question.Target)
		if target == "" {
			continue
		}
		refs = append(refs, operatorDigestSourceRef{
			ID:   target,
			Kind: operatorDigestTargetKind(target),
			Name: target,
		})
	}
	return refs
}

func operatorDigestTargetKind(target string) string {
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

func dedupeOperatorDigestSourceRefs(refs []operatorDigestSourceRef) []operatorDigestSourceRef {
	seen := make(map[string]struct{}, len(refs))
	out := make([]operatorDigestSourceRef, 0, len(refs))
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

func operatorDigestArtifactID(artifact operatorDigestArtifact) string {
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
