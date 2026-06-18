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
// graph Function entities they span. The parser's FunctionID carries the function
// name but not the entity uid, so each endpoint is resolved by name within the
// file. A name that is not unique within the file, or that does not materialize
// as an entity, is treated as unresolved and the finding is dropped (no orphan or
// mis-attributed edge). Empty when the parser emitted no interproc findings, so
// the snapshot is byte-identical when the value-flow gate is off.
func buildInterprocTaintEvidence(repoPath string, parsedFiles []map[string]any, entities []content.EntityRecord) []InterprocTaintEvidenceSnapshot {
	// Per-file unique function-name -> uid. A name seen twice in one file is
	// marked ambiguous and never resolves.
	uidByName := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, entity := range entities {
		if entity.EntityType != taintEvidenceFunctionLabel {
			continue
		}
		key := entity.Path + "\x00" + entity.EntityName
		if _, exists := uidByName[key]; exists {
			ambiguous[key] = struct{}{}
			continue
		}
		uidByName[key] = entity.EntityID
	}
	resolve := func(relativePath, name string) (string, bool) {
		if name == "" {
			return "", false
		}
		key := relativePath + "\x00" + name
		if _, bad := ambiguous[key]; bad {
			return "", false
		}
		uid, ok := uidByName[key]
		return uid, ok
	}

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
			sourceName := functionIDName(snapshotPayloadString(finding, "source_func"))
			sinkName := functionIDName(snapshotPayloadString(finding, "sink_func"))
			sourceUID, okSource := resolve(relativePath, sourceName)
			sinkUID, okSink := resolve(relativePath, sinkName)
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
			})
		}
	}
	return evidence
}

// functionIDName returns the function name component of a value-flow FunctionID
// (repo\x1fpkg\x1freceiver\x1fname): the substring after the last separator.
func functionIDName(functionID string) string {
	if functionID == "" {
		return ""
	}
	if idx := strings.LastIndexByte(functionID, '\x1f'); idx >= 0 {
		return functionID[idx+1:]
	}
	return functionID
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
