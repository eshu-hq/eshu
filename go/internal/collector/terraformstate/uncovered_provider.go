// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"sort"
	"strings"
)

// warningKindProviderSchemaNotCovered names a resource type the loaded provider
// schema bundle does not cover at all (#5870).
const warningKindProviderSchemaNotCovered = "provider_schema_not_covered"

// reasonProviderNotInSchemaBundle is the single reason this warning carries.
// It is its own constant because tfstatewarning.Classify keys severity and
// actionability off the (kind, reason) pair, and a typo there silently drops
// the row to unclassified.
const reasonProviderNotInSchemaBundle = "provider_not_in_schema_bundle"

// recordUncoveredResourceType notes that the schema bundle does not cover
// resourceType at all, so an operator can be told which provider to add.
//
// This detector exists because the #5870 identity-join-key exemption removes
// the only symptom a stale bundle used to produce. Before it, an uncovered
// provider announced itself loudly and wrongly: every one of its resources lost
// its `arn`, dropped out of the drift join, and surfaced as
// orphaned_cloud_resource. The exemption fixes that wrong answer -- and in
// doing so makes a stale bundle SILENT. Without this warning the fix would be a
// permanent crutch nobody notices, and every non-identity scalar of that
// provider would still be redacted with nothing saying why.
//
// It fires on the resource TYPE being absent, never on a known type missing one
// attribute. Those are different problems: an absent type is a provider the
// bundle does not carry, while a missing attribute on a covered type is a
// bundle that is merely older than the provider. Reporting the second here
// would put a provider-coverage warning on ordinary version skew and teach
// operators to ignore the signal.
//
// Detection needs SchemaResourceTypeReporter. A resolver without it -- fixture
// stubs, mostly -- records nothing rather than guessing, exactly like the
// eshu_dp_tfstate_schema_resolver_entries gauge degrades for the same reason.
func (p *stateParser) recordUncoveredResourceType(resourceType string) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return
	}
	reporter, ok := p.options.SchemaResolver.(SchemaResourceTypeReporter)
	if !ok || reporter.HasResourceType(resourceType) {
		return
	}
	if providerFromResourceType(resourceType) == "" {
		return
	}
	if p.uncoveredResourceTypes == nil {
		p.uncoveredResourceTypes = map[string]int64{}
	}
	p.uncoveredResourceTypes[resourceType]++
}

// flushUncoveredProviderWarnings emits one warning per uncovered resource type,
// in sorted order so a replay of the same state file produces byte-identical
// facts.
//
// One row per resource type rather than per attribute or per resource: the
// operator action is "add this provider to the bundle", which is the same
// however many attributes or instances were affected. The occurrence count
// rides along so the size of the gap is still visible.
func (p *stateParser) flushUncoveredProviderWarnings() error {
	if len(p.uncoveredResourceTypes) == 0 {
		return nil
	}
	resourceTypes := make([]string, 0, len(p.uncoveredResourceTypes))
	for resourceType := range p.uncoveredResourceTypes {
		resourceTypes = append(resourceTypes, resourceType)
	}
	sort.Strings(resourceTypes)
	for _, resourceType := range resourceTypes {
		count := p.uncoveredResourceTypes[resourceType]
		if count <= 0 {
			continue
		}
		if err := p.emitWarning(warningPayload{
			WarningKind: warningKindProviderSchemaNotCovered,
			Reason:      reasonProviderNotInSchemaBundle,
			Source:      "resources." + resourceType,
			Details: map[string]any{
				"resource_type":    resourceType,
				"provider":         providerFromResourceType(resourceType),
				"occurrence_count": count,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}
