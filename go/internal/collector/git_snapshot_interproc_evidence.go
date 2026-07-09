// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// interprocEvidenceFactKind is the fact kind for one resolved cross-function
// taint finding. The reducer projects these as a source->sink evidence relation
// between two Function nodes, never as canonical truth.
const interprocEvidenceFactKind = "code_interproc_evidence"

// buildInterprocTaintEvidence resolves each file's interproc_findings to the
// graph Function entities they span. The parser's FunctionID carries the
// function's receiver and name but not the entity uid, so each endpoint is
// resolved by (receiver, name) within the file. The receiver is the method's
// class context, mirrored on the function entity as class_context metadata; both
// the entity and the finding derive it from the same goReceiverContext helper, so
// same-named methods on different receivers (e.g. (A) Handle vs (B) Handle)
// disambiguate cleanly instead of colliding on the bare name. A (receiver, name)
// pair that is not unique within the file, or that does not materialize as an
// entity, is treated as unresolved and the finding is dropped (no orphan or
// mis-attributed edge). Empty when the parser emitted no interproc findings, so
// the snapshot is byte-identical when the value-flow gate is off.
// newFunctionUIDResolver builds a per-repo resolver from the function entities:
// given a function's (relative path, receiver, name) it returns the graph Function
// uid. A (path, receiver, name) triple that materializes as more than one entity
// is ambiguous and never resolves, so a finding never attaches to the wrong node.
// The receiver is the entity's class context, the same component the FunctionID
// carries, so both the per-file evidence path and the cross-repo summary path
// resolve uids identically.
func newFunctionUIDResolver(entities []content.EntityRecord) func(relativePath, receiver, name string) (string, bool) {
	uidByFunction := make(map[string]string)
	ambiguous := make(map[string]struct{})
	functionKey := func(relativePath, receiver, name string) string {
		return relativePath + "\x00" + receiver + "\x00" + name
	}
	for _, entity := range entities {
		if entity.EntityType != taintEvidenceFunctionLabel {
			continue
		}
		key := functionKey(entity.Path, entityClassContext(entity), entity.EntityName)
		if _, exists := uidByFunction[key]; exists {
			ambiguous[key] = struct{}{}
			continue
		}
		uidByFunction[key] = entity.EntityID
	}
	return func(relativePath, receiver, name string) (string, bool) {
		if name == "" {
			return "", false
		}
		key := functionKey(relativePath, receiver, name)
		if _, bad := ambiguous[key]; bad {
			return "", false
		}
		uid, ok := uidByFunction[key]
		return uid, ok
	}
}

// buildInterprocTaintEvidence resolves each file's interproc_findings to the
// graph Function entities they span, using the shared functionUIDResolver built
// once per snapshot by newFunctionUIDResolver. The resolver is read-only; a
// finding whose endpoint cannot be resolved is dropped.
func buildInterprocTaintEvidence(repoPath string, parsedFiles []map[string]any, functionUIDResolver func(relativePath, receiver, name string) (string, bool)) []InterprocTaintEvidenceSnapshot {
	var evidence []InterprocTaintEvidenceSnapshot
	for _, parsedFile := range parsedFiles {
		findings, _ := parsedFile["interproc_findings"].([]map[string]any)
		if len(findings) == 0 {
			continue
		}
		relativePath, err := filepath.Rel(repoPath, snapshotPayloadString(parsedFile, "path"))
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))

		for _, finding := range findings {
			sourceReceiver, sourceName := functionIDReceiverName(snapshotPayloadString(finding, "source_func"))
			sinkReceiver, sinkName := functionIDReceiverName(snapshotPayloadString(finding, "sink_func"))
			sourceUID, okSource := functionUIDResolver(relativePath, sourceReceiver, sourceName)
			sinkUID, okSink := functionUIDResolver(relativePath, sinkReceiver, sinkName)
			if !okSource || !okSink {
				continue
			}
			cloud, _ := finding["cloud"].(bool)
			evidence = append(evidence, InterprocTaintEvidenceSnapshot{
				SourceFunctionUID:  sourceUID,
				SinkFunctionUID:    sinkUID,
				RelativePath:       relativePath,
				SourceFunctionName: sourceName,
				SinkFunctionName:   sinkName,
				Language:           snapshotPayloadString(finding, "lang", "language"),
				SinkKind:           snapshotPayloadString(finding, "sink_kind"),
				SourceKind:         snapshotPayloadString(finding, "source_kind"),
				Confidence:         snapshotPayloadFloat(finding, "confidence"),
				Cloud:              cloud,
				WhyTrail:           interprocWhyTrailFromFinding(relativePath, finding, functionUIDResolver),
				WhyTrailTruncated:  snapshotPayloadBool(finding, "why_trail_truncated"),
			})
		}
	}
	return evidence
}

func interprocWhyTrailFromFinding(
	relativePath string,
	finding map[string]any,
	resolve func(relativePath, receiver, name string) (string, bool),
) []map[string]any {
	rawTrail := snapshotPayloadMapSlice(finding, "why_trail")
	if len(rawTrail) == 0 {
		return nil
	}
	trail := make([]map[string]any, 0, len(rawTrail))
	for index, step := range rawTrail {
		functionID := snapshotPayloadString(step, "function_id")
		receiver, name := functionIDReceiverName(functionID)
		role := "intermediate"
		if index == 0 {
			role = "source"
		}
		if index == len(rawTrail)-1 {
			role = "sink"
		}
		out := map[string]any{
			"role":        role,
			"function_id": functionID,
			"slot_kind":   snapshotPayloadString(step, "slot_kind"),
		}
		if name != "" {
			out["function_name"] = name
		}
		if uid, ok := resolve(relativePath, receiver, name); ok {
			out["function_uid"] = uid
		}
		if slotIndex := snapshotPayloadInt(step, "slot_index"); slotIndex != 0 || snapshotPayloadString(step, "slot_kind") == "param" {
			out["slot_index"] = slotIndex
		}
		if slotName := snapshotPayloadString(step, "slot_name"); slotName != "" {
			out["slot_name"] = slotName
		}
		trail = append(trail, out)
	}
	return trail
}

func snapshotPayloadMapSlice(payload map[string]any, key string) []map[string]any {
	switch typed := payload[key].(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	default:
		return nil
	}
}

func snapshotPayloadBool(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

// functionIDReceiverName splits a value-flow FunctionID
// (repo\x1fpkg\x1freceiver\x1fname) into its receiver and name components: the
// last separator-delimited field is the name and the field before it is the
// receiver (empty for a top-level function). A FunctionID with no separator is
// treated as a bare name with no receiver.
func functionIDReceiverName(functionID string) (receiver, name string) {
	if functionID == "" {
		return "", ""
	}
	parts := strings.Split(functionID, "\x1f")
	name = parts[len(parts)-1]
	if len(parts) >= 2 {
		receiver = parts[len(parts)-2]
	}
	return receiver, name
}

// entityClassContext returns the receiver/class context recorded on a function
// entity (the class_context metadata key, set by the parser from the same
// goReceiverContext helper that builds a FunctionID's receiver component). It is
// the empty string for a top-level function or when the metadata is absent.
func entityClassContext(entity content.EntityRecord) string {
	if entity.Metadata == nil {
		return ""
	}
	context, _ := entity.Metadata["class_context"].(string)
	return context
}

// interprocEvidenceFactEnvelope builds the fact envelope for one resolved cross-
// function finding. The stable key is unique per (source, sink, kinds) so
// re-emission is idempotent.
func interprocEvidenceFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	evidence InterprocTaintEvidenceSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_kind":           interprocEvidenceFactKind,
		"source_function_uid":  evidence.SourceFunctionUID,
		"sink_function_uid":    evidence.SinkFunctionUID,
		"repo_id":              repoID,
		"relative_path":        evidence.RelativePath,
		"source_function_name": evidence.SourceFunctionName,
		"sink_function_name":   evidence.SinkFunctionName,
		"language":             evidence.Language,
		"sink_kind":            evidence.SinkKind,
		"source_kind":          evidence.SourceKind,
		"confidence":           evidence.Confidence,
	}
	if evidence.Cloud {
		payload["cloud"] = true
	}
	if len(evidence.WhyTrail) > 0 {
		payload["why_trail"] = evidence.WhyTrail
	}
	if evidence.WhyTrailTruncated {
		payload["why_trail_truncated"] = true
	}

	factKey := interprocEvidenceFactKind + ":" + evidence.SourceFunctionUID +
		":" + evidence.SinkFunctionUID +
		":" + evidence.SinkKind +
		":" + evidence.SourceKind
	return factEnvelope(
		interprocEvidenceFactKind,
		scopeID,
		generationID,
		observedAt,
		factKey,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(evidence.RelativePath)),
	)
}
