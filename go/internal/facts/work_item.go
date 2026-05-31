package facts

import "slices"

const (
	// WorkItemRecordFactKind identifies one provider work item, such as a Jira
	// issue, as source-reported work tracking evidence.
	WorkItemRecordFactKind = "work_item.record"
	// WorkItemTransitionFactKind identifies one provider work item changelog
	// transition or field change as source-reported lifecycle evidence.
	WorkItemTransitionFactKind = "work_item.transition"
	// WorkItemExternalLinkFactKind identifies one provider remote link attached
	// to a work item as source-reported cross-system evidence.
	WorkItemExternalLinkFactKind = "work_item.external_link"

	// WorkItemSchemaVersionV1 is the first work-item fact schema.
	WorkItemSchemaVersionV1 = "1.0.0"
)

var workItemFactKinds = []string{
	WorkItemRecordFactKind,
	WorkItemTransitionFactKind,
	WorkItemExternalLinkFactKind,
}

var workItemSchemaVersions = map[string]string{
	WorkItemRecordFactKind:       WorkItemSchemaVersionV1,
	WorkItemTransitionFactKind:   WorkItemSchemaVersionV1,
	WorkItemExternalLinkFactKind: WorkItemSchemaVersionV1,
}

// WorkItemFactKinds returns the accepted work-item fact kinds in
// source-contract order.
func WorkItemFactKinds() []string {
	return slices.Clone(workItemFactKinds)
}

// WorkItemSchemaVersion returns the schema version for a work-item fact kind.
func WorkItemSchemaVersion(factKind string) (string, bool) {
	version, ok := workItemSchemaVersions[factKind]
	return version, ok
}
