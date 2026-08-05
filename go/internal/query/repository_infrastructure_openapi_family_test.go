// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
	"testing"
)

// repositoryInfrastructureFamilyLeadingWordPattern extracts a canonical
// entity_type name's leading CamelCase word: a leading capital plus the
// lowercase/digit run before the next capital. "TerraformResource" yields
// "Terraform"; "K8sResource" yields "K8s"; "ArgoCDApplication" yields "Argo"
// (the run stops at "CD", the next capital pair).
var repositoryInfrastructureFamilyLeadingWordPattern = regexp.MustCompile(`^[A-Z][a-z0-9]*`)

// repositoryInfrastructureFamilyAliases maps a canonical type's leading
// CamelCase word to the family name the OpenAPI description actually uses,
// for the one case where they differ. Every other canonical type's leading
// word is already a substring of its family name in prose -- "Argo" from
// ArgoCDApplication is a substring of "ArgoCD", "Cloud" from
// CloudFormationResource is a substring of "CloudFormation" -- so no other
// alias is needed. Adding one here does not create a second hand-maintained
// type enumeration: the map is keyed by family word, not by the 20 canonical
// types, and it only grows when a future family's leading word stops being a
// substring of its own prose name (#5764 round-9 P2-2 review follow-up).
var repositoryInfrastructureFamilyAliases = map[string]string{
	"K8s": "Kubernetes",
}

// repositoryInfrastructureTypeFamily derives the family token this test
// checks against OpenAPI prose from a canonical entity_type name, with no
// hand-written family-to-type map: the token comes straight out of the type
// name Go already declares in repositoryInfrastructureEntityTypes.
func repositoryInfrastructureTypeFamily(entityType string) string {
	family := repositoryInfrastructureFamilyLeadingWordPattern.FindString(entityType)
	if alias, ok := repositoryInfrastructureFamilyAliases[family]; ok {
		return alias
	}
	return family
}

// repositoryInfrastructureDescriptionPattern extracts the "infrastructure"
// field's OpenAPI description text from openAPIComponentsWorkloadSession
// (openapi_components_workload_session.go), so this test reads the string
// that actually ships instead of a hand-copied one that can drift from it.
var repositoryInfrastructureDescriptionPattern = regexp.MustCompile(
	`(?s)"infrastructure":\s*\{.*?"description":\s*"([^"]*)"`,
)

// TestRepositoryInfrastructureOpenAPIDescriptionNamesEveryCanonicalFamily
// substring-matches each canonical type's family token against the
// "infrastructure" field's OpenAPI description prose, so a canonical type
// whose family is missing from that text fails the build instead of shipping
// silently. This is the mechanical cross-check the round-8 entry in
// AGENTS-evidence-history-4.md declined to add, on the premise that it would
// need a fifth hand-maintained family-to-type-prefix map -- it does not: the
// family token is derived from the type name itself
// (repositoryInfrastructureTypeFamily), with one documented alias
// (K8s -> Kubernetes). The three prose copies this cannot reach --
// repository_infrastructure.go's doc comment,
// docs/public/reference/http-api/repositories-ingesters-bundles.md, and
// docs/public/reference/telemetry/graph-read-safety.md -- stay a known,
// disclosed gap; grep them by hand when this list changes (#5764 round-9
// P2-2 review follow-up).
//
// substring-match false-pass window (#5764 round-10 P3-1 review
// follow-up): the check above proves a canonical type's family is present
// in the description text, not that the description's own family word is
// exactly that token -- a description reworded to a longer word sharing the
// same prefix would still pass. Two canonical families are live instances:
// "Argo" (from ArgoCDApplication) is a strict prefix of "ArgoCD", so a
// future ArgoWorkflow type or a description that only ever said "Argo"
// would pass without the "ArgoCD" text this list actually intends; "Cloud"
// (from CloudFormationResource) is a strict prefix of "CloudFormation", so a
// future CloudRunService type or a "CloudWatch"-only description passes the
// same way. A wrong-family derivation or a genuinely missing alias still
// fails loudly -- this window is narrower than that -- but it is real and
// undetected by this test.
func TestRepositoryInfrastructureOpenAPIDescriptionNamesEveryCanonicalFamily(t *testing.T) {
	t.Parallel()

	match := repositoryInfrastructureDescriptionPattern.FindStringSubmatch(openAPIComponentsWorkloadSession)
	if match == nil {
		t.Fatalf("openAPIComponentsWorkloadSession: infrastructure field description not found")
	}
	description := match[1]

	for _, entityType := range repositoryInfrastructureEntityTypes {
		family := repositoryInfrastructureTypeFamily(entityType)
		if family == "" {
			t.Fatalf("repositoryInfrastructureTypeFamily(%q) = \"\", want a non-empty leading word", entityType)
		}
		if !strings.Contains(description, family) {
			t.Errorf("infrastructure description %q missing family %q for canonical type %q", description, family, entityType)
		}
	}
}
