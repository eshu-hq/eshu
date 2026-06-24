// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// serviceCatalogIncidentEvidenceLoader returns the service-scoped incident
// routing evidence loader used to materialize the service incidents evidence
// family (#1989), or nil when the family cannot be sourced. The family is only
// wired when both the service generation lineage writer is present and an
// incident evidence loader is configured, so incident evidence is purely additive
// and never blocks the ownership/deployment/runtime/dependencies/docs lineage.
func serviceCatalogIncidentEvidenceLoader(
	handlers DefaultHandlers,
) ServiceScopedIncidentEvidenceLoader {
	if handlers.ServiceMaterializationWriter == nil || handlers.ServiceIncidentEvidenceLoader == nil {
		return nil
	}
	return handlers.ServiceIncidentEvidenceLoader
}
