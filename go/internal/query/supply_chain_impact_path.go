// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

func buildSupplyChainImpactPath(
	row SupplyChainImpactExplanationRow,
	missing []string,
) []SupplyChainImpactPathHop {
	var hops []SupplyChainImpactPathHop
	for _, hop := range row.Finding.EvidencePath {
		hops = append(hops, SupplyChainImpactPathHop{
			Hop:             hop,
			Status:          "present",
			EvidenceFactIDs: evidenceFactIDsForHop(hop, row),
		})
	}
	hops = append(hops, semanticSupplyChainImpactHops(row, missing)...)
	for _, reason := range missing {
		hops = append(hops, SupplyChainImpactPathHop{
			Hop:             reason,
			Status:          "missing_evidence",
			MissingEvidence: []string{reason},
		})
	}
	if len(hops) == 0 {
		return nil
	}
	return hops
}

func semanticSupplyChainImpactHops(
	row SupplyChainImpactExplanationRow,
	missing []string,
) []SupplyChainImpactPathHop {
	repositoryEvidence := evidenceFactIDsForSemanticHop(row, "repository")
	imageEvidence := evidenceFactIDsForSemanticHop(row, "image")
	workloadEvidence := evidenceFactIDsForSemanticHop(row, "workload")
	deploymentEvidence := evidenceFactIDsForSemanticHop(row, "deployment")
	serviceEvidence := evidenceFactIDsForSemanticHop(row, "service")
	environmentEvidence := evidenceFactIDsForSemanticHop(row, "environment")
	return []SupplyChainImpactPathHop{
		semanticSupplyChainImpactHop(
			"repository",
			row.Finding.RepositoryID != "" || len(repositoryEvidence) > 0,
			repositoryEvidence,
			missingReasonForSemanticHop("repository", missing),
		),
		semanticSupplyChainImpactHop(
			"image",
			row.Finding.SubjectDigest != "" || row.Finding.ImageRef != "" || len(imageEvidence) > 0,
			imageEvidence,
			missingReasonForSemanticHop("image", missing),
		),
		semanticSupplyChainImpactHop(
			"workload",
			len(row.Finding.WorkloadIDs) > 0 || len(workloadEvidence) > 0,
			workloadEvidence,
			missingReasonForSemanticHop("workload", missing),
		),
		semanticSupplyChainImpactHop(
			"deployment",
			len(row.Finding.DeploymentIDs) > 0 || len(deploymentEvidence) > 0,
			deploymentEvidence,
			missingReasonForSemanticHop("deployment", missing),
		),
		semanticSupplyChainImpactHop(
			"service",
			len(row.Finding.ServiceIDs) > 0 || len(serviceEvidence) > 0,
			serviceEvidence,
			missingReasonForSemanticHop("service", missing),
		),
		semanticSupplyChainImpactHop(
			"environment",
			len(row.Finding.Environments) > 0 || len(environmentEvidence) > 0,
			environmentEvidence,
			missingReasonForSemanticHop("environment", missing),
		),
	}
}

func semanticSupplyChainImpactHop(
	hop string,
	present bool,
	evidenceFactIDs []string,
	missingEvidence []string,
) SupplyChainImpactPathHop {
	if present {
		return SupplyChainImpactPathHop{
			Hop:             hop,
			Status:          "present",
			EvidenceFactIDs: evidenceFactIDs,
		}
	}
	return SupplyChainImpactPathHop{
		Hop:             hop,
		Status:          "missing_evidence",
		MissingEvidence: missingEvidence,
	}
}

func evidenceFactIDsForHop(hop string, row SupplyChainImpactExplanationRow) []string {
	var factIDs []string
	for _, fact := range row.EvidenceFacts {
		if fact.FactKind == hop {
			factIDs = append(factIDs, fact.FactID)
		}
	}
	return explanationUniqueStrings(factIDs)
}

// evidenceFactIDsForSemanticHop tests presence of a hop's anchor key on each
// evidence fact rather than reading a typed field value, so it stays on the
// raw payload path (#4795 W2b): every anchor key here (repository_id,
// subject_digest/digest/image_ref/artifact_digest, workload_id,
// deployment_id, service_id, environment) is chiefly carried by
// reducer-derived kinds (reducer_container_image_identity,
// reducer_platform_materialization, reducer_workload_identity,
// reducer_service_catalog_correlation) with no sdk/go/factschema struct yet
// (#4784 ADR, docs/internal/design/4784-reducer-derived-fact-governance.md);
// the "service" case below is already scoped to the exact reducer-derived
// kind it targets.
func evidenceFactIDsForSemanticHop(
	row SupplyChainImpactExplanationRow,
	hop string,
) []string {
	var factIDs []string
	for _, fact := range row.EvidenceFacts {
		switch hop {
		case "repository":
			if StringVal(fact.Payload, "repository_id") != "" {
				factIDs = append(factIDs, fact.FactID)
			}
		case "image":
			if StringVal(fact.Payload, "subject_digest") != "" ||
				StringVal(fact.Payload, "digest") != "" ||
				StringVal(fact.Payload, "image_ref") != "" ||
				StringVal(fact.Payload, "artifact_digest") != "" {
				factIDs = append(factIDs, fact.FactID)
			}
		case "workload":
			if StringVal(fact.Payload, "workload_id") != "" ||
				payloadEntityKeyHasPrefix(fact.Payload, "workload:") {
				factIDs = append(factIDs, fact.FactID)
			}
		case "deployment":
			if StringVal(fact.Payload, "deployment_id") != "" ||
				payloadEntityKeyHasPrefix(fact.Payload, "deployment:") {
				factIDs = append(factIDs, fact.FactID)
			}
		case "service":
			// reducer_service_catalog_correlation (serviceCatalogCorrelationFactKind)
			// is reducer-derived with no factschema struct yet (#4784 ADR);
			// this stays a raw presence check.
			if StringVal(fact.Payload, "service_id") != "" ||
				(fact.FactKind == serviceCatalogCorrelationFactKind &&
					StringVal(fact.Payload, "entity_ref") != "") {
				factIDs = append(factIDs, fact.FactID)
			}
		case "environment":
			if StringVal(fact.Payload, "environment") != "" {
				factIDs = append(factIDs, fact.FactID)
			}
		}
	}
	return explanationUniqueStrings(factIDs)
}

func missingReasonForSemanticHop(hop string, missing []string) []string {
	for _, reason := range missing {
		switch hop {
		case "repository":
			if reason == "repository dependency evidence missing" {
				return []string{reason}
			}
		case "image":
			if reason == "image or SBOM attachment evidence missing" ||
				strings.HasPrefix(reason, "image identity evidence ") {
				return []string{reason}
			}
		case "workload":
			if reason == "workload evidence missing" {
				return []string{reason}
			}
		case "deployment":
			if reason == "deployment exposure evidence missing" ||
				reason == "runtime deployment evidence not linked to vulnerable package" ||
				strings.HasPrefix(reason, "deployment evidence ") {
				return []string{reason}
			}
		case "service":
			if reason == "service evidence missing" ||
				reason == serviceCatalogCorrelationMissingReason ||
				reason == serviceCatalogAnchorMissingReason ||
				strings.HasPrefix(reason, "service catalog evidence ") {
				return []string{reason}
			}
		case "environment":
			if reason == "environment evidence missing" {
				return []string{reason}
			}
		}
	}
	return []string{hop + " evidence missing"}
}

func payloadEntityKeyHasPrefix(payload map[string]any, prefix string) bool {
	for _, entityKey := range StringSliceVal(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, prefix) {
			return true
		}
	}
	return false
}

func supplyChainImpactPathMissingEvidence(missing []string) []string {
	var out []string
	for _, reason := range missing {
		if supplyChainImpactPathMissingReason(reason) {
			out = append(out, reason)
		}
	}
	return explanationUniqueStrings(out)
}

func supplyChainImpactPathMissingReason(reason string) bool {
	switch reason {
	case "package version evidence missing",
		"repository dependency evidence missing",
		"image or SBOM attachment evidence missing",
		"deployment exposure evidence missing",
		"runtime deployment evidence not linked to vulnerable package",
		"environment evidence missing",
		"workload evidence missing",
		"service evidence missing",
		serviceCatalogCorrelationMissingReason,
		serviceCatalogAnchorMissingReason:
		return true
	}
	for _, prefix := range []string{
		"image identity evidence ",
		"deployment evidence ",
		"service catalog evidence ",
	} {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}
