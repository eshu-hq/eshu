package serviceintel

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// FromServiceStory maps a get_service_story dossier response into the report
// ReportInput it sources: the subject identity plus the identity,
// code_to_runtime, and deployment_config sections. It is a faithful,
// side-effect-free translation — it reads only confirmed dossier fields and
// section cardinalities, never invents evidence, and carries the source truth
// envelope verbatim onto each section. Callers append supply-chain and incident
// sections from their own routes before calling Compose.
//
// A nil truth marks every section unsupported, reflecting a dossier that could
// not be classified rather than fabricating confidence.
func FromServiceStory(dossier map[string]any, truth *query.TruthEnvelope) ReportInput {
	if dossier == nil {
		return ReportInput{}
	}
	identity := subMap(dossier, "service_identity")
	subject := ReportSubject{
		ServiceID:   query.StringVal(identity, "service_id"),
		ServiceName: query.StringVal(identity, "service_name"),
		RepoID:      query.StringVal(identity, "repo_id"),
		RepoName:    query.StringVal(identity, "repo_name"),
	}
	truncated := query.BoolVal(subMap(dossier, "result_limits"), "truncated")
	serviceHandle, hasHandle := serviceEntityHandle(subject)

	entrypoints := sliceLen(dossier, "entrypoints")
	networkPaths := sliceLen(dossier, "network_paths")
	codeToRuntime := entrypoints > 0 || networkPaths > 0
	lanes := sliceLen(dossier, "deployment_lanes")

	input := ReportInput{Subject: subject}
	input.Sections = []SectionInput{
		{
			Kind:        SectionIdentity,
			Summary:     identitySummary(subject, query.StringVal(identity, "kind")),
			Truth:       truth,
			Evidence:    handlesIf(hasHandle, serviceHandle),
			Limitations: query.StringSliceVal(identity, "limitations"),
			Truncated:   truncated,
			NoEvidence:  !hasHandle,
		},
		{
			Kind:       SectionCodeToRuntime,
			Summary:    codeToRuntimeSummary(subject, entrypoints, networkPaths),
			Truth:      truth,
			Evidence:   handlesIf(hasHandle && codeToRuntime, serviceHandle),
			Truncated:  truncated,
			NoEvidence: !codeToRuntime,
		},
		{
			Kind:       SectionDeploymentConfig,
			Summary:    deploymentSummary(subject, lanes),
			Truth:      truth,
			Evidence:   handlesIf(hasHandle && lanes > 0, serviceHandle),
			Truncated:  truncated,
			NoEvidence: lanes == 0,
		},
	}
	return input
}

// serviceEntityHandle builds the evidence handle that addresses the service node
// itself. It is the canonical, resolvable pointer behind a service-story claim
// (the graph node is the evidence). It returns ok=false when no identifier is
// available, so the adapter never emits an empty handle.
func serviceEntityHandle(subject ReportSubject) (query.EvidenceCitationHandle, bool) {
	entityID := strings.TrimSpace(subject.ServiceID)
	repoID := strings.TrimSpace(subject.RepoID)
	if entityID == "" && repoID == "" {
		return query.EvidenceCitationHandle{}, false
	}
	return query.EvidenceCitationHandle{
		Kind:           "service",
		EntityID:       entityID,
		RepoID:         repoID,
		EvidenceFamily: "service_story",
		Reason:         "service identity resolved from the service story dossier",
	}, true
}

func identitySummary(subject ReportSubject, kind string) string {
	name := subjectName(subject)
	if k := strings.TrimSpace(kind); k != "" {
		return fmt.Sprintf("Service %s is a %s.", name, k)
	}
	return fmt.Sprintf("Service %s.", name)
}

func codeToRuntimeSummary(subject ReportSubject, entrypoints, networkPaths int) string {
	if entrypoints == 0 && networkPaths == 0 {
		return ""
	}
	return fmt.Sprintf("Service %s exposes %d entrypoint(s) over %d evidence-backed network path(s).",
		subjectName(subject), entrypoints, networkPaths)
}

func deploymentSummary(subject ReportSubject, lanes int) string {
	if lanes == 0 {
		return ""
	}
	return fmt.Sprintf("Service %s deploys across %d evidence-backed lane(s).", subjectName(subject), lanes)
}

// handlesIf returns a single-handle slice when the condition holds, else nil, so
// a section carries evidence only when the dossier actually supports it.
func handlesIf(ok bool, handle query.EvidenceCitationHandle) []query.EvidenceCitationHandle {
	if !ok {
		return nil
	}
	return []query.EvidenceCitationHandle{handle}
}

// subMap returns m[key] as a map[string]any, or nil when absent or another type.
func subMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if nested, ok := m[key].(map[string]any); ok {
		return nested
	}
	return nil
}

// sliceLen returns the length of m[key] whether it decoded as []any (JSON) or
// []map[string]any (in-process), and 0 otherwise. It reads only cardinality, so
// it never depends on element field shapes.
func sliceLen(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch value := m[key].(type) {
	case []any:
		return len(value)
	case []map[string]any:
		return len(value)
	default:
		return 0
	}
}
