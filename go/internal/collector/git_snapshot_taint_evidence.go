// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// taintEvidenceFunctionLabel is the graph entity label a taint finding's
// function resolves to. Findings are reported per function, which materializes as
// a Function entity.
const taintEvidenceFunctionLabel = "Function"

// taintEvidenceFactKind is the fact kind for one resolved intraprocedural taint
// finding. The reducer projects these as evidence nodes/edges against the
// Function node, never as canonical truth.
const taintEvidenceFactKind = "code_taint_evidence"

// buildTaintEvidence resolves each file's intraprocedural taint_findings to the
// graph Function entity they concern, using the same (path, label, name, line)
// identity the entity materialization assigns. A finding whose function cannot be
// resolved to an entity id is dropped (no orphan evidence). The result is empty
// when the parser emitted no taint findings, so the snapshot is byte-identical
// when the value-flow gate is off.
func buildTaintEvidence(repoPath string, parsedFiles []map[string]any, entities []content.EntityRecord) []TaintEvidenceSnapshot {
	lookup := make(map[string]string, len(entities))
	for _, entity := range entities {
		key := entityLookupKey(entity.Path, entity.EntityType, entity.EntityName, entity.StartLine)
		lookup[key] = entity.EntityID
	}

	var evidence []TaintEvidenceSnapshot
	for _, parsedFile := range parsedFiles {
		findings, _ := parsedFile["taint_findings"].([]map[string]any)
		if len(findings) == 0 {
			continue
		}
		absolutePath := snapshotPayloadString(parsedFile, "path")
		relativePath, err := filepath.Rel(repoPath, absolutePath)
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))

		for _, finding := range findings {
			functionName := snapshotPayloadString(finding, "function_name")
			line := snapshotPayloadInt(finding, "line_number")
			key := entityLookupKey(relativePath, taintEvidenceFunctionLabel, functionName, line)
			functionUID, ok := lookup[key]
			if !ok {
				// The function did not materialize as an entity; drop the finding
				// rather than emit evidence with no node to attach to.
				continue
			}
			evidence = append(evidence, TaintEvidenceSnapshot{
				FunctionUID:  functionUID,
				RelativePath: relativePath,
				FunctionName: functionName,
				Language:     snapshotPayloadString(finding, "lang", "language"),
				Kind:         snapshotPayloadString(finding, "kind"),
				SinkKind:     snapshotPayloadString(finding, "sink_kind"),
				SourceKind:   snapshotPayloadString(finding, "source_kind"),
				Binding:      snapshotPayloadString(finding, "binding"),
				SourceLine:   snapshotPayloadInt(finding, "source_line"),
				SinkLine:     snapshotPayloadInt(finding, "sink_line"),
				Confidence:   snapshotPayloadFloat(finding, "confidence"),
				ClassContext: snapshotPayloadString(finding, "class_context"),
				SinkLabel:    snapshotPayloadString(finding, "sink_label"),
				SourceLabel:  snapshotPayloadString(finding, "source_label"),
				GuardReason:  snapshotPayloadString(finding, "guard_reason"),
			})
		}
	}
	return evidence
}

// taintEvidenceFactEnvelope builds the fact envelope for one resolved taint
// finding. The stable key is unique per finding within a function so re-emission
// is idempotent: a given source-to-sink finding maps to one durable fact.
func taintEvidenceFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	evidence TaintEvidenceSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_kind":    taintEvidenceFactKind,
		"function_uid":  evidence.FunctionUID,
		"repo_id":       repoID,
		"relative_path": evidence.RelativePath,
		"function_name": evidence.FunctionName,
		"language":      evidence.Language,
		"kind":          evidence.Kind,
		"sink_kind":     evidence.SinkKind,
		"source_kind":   evidence.SourceKind,
		"binding":       evidence.Binding,
		"source_line":   evidence.SourceLine,
		"sink_line":     evidence.SinkLine,
		"confidence":    evidence.Confidence,
	}
	if evidence.ClassContext != "" {
		payload["class_context"] = evidence.ClassContext
	}
	if evidence.SinkLabel != "" {
		payload["sink_label"] = evidence.SinkLabel
	}
	if evidence.SourceLabel != "" {
		payload["source_label"] = evidence.SourceLabel
	}
	if evidence.GuardReason != "" {
		payload["guard_reason"] = evidence.GuardReason
	}

	factKey := taintEvidenceFactKind + ":" + evidence.FunctionUID +
		":" + strconv.Itoa(evidence.SourceLine) +
		":" + strconv.Itoa(evidence.SinkLine) +
		":" + evidence.SinkKind +
		":" + evidence.SourceKind +
		":" + evidence.Binding
	return factEnvelope(
		taintEvidenceFactKind,
		scopeID,
		generationID,
		observedAt,
		factKey,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(evidence.RelativePath)),
	)
}

// snapshotPayloadFloat reads a float64 payload field, defaulting to zero.
func snapshotPayloadFloat(payload map[string]any, key string) float64 {
	value, _ := payload[key].(float64)
	return value
}
