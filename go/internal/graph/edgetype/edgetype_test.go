// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package edgetype

import "testing"

// TestEdgeTypeStringParity pins every registered edge type to its exact
// historical Cypher relationship-type string. A failure here means a constant
// was renamed away from the byte-identical graph-wire contract, which would
// silently break stored edges and every reader that matches the old type.
func TestEdgeTypeStringParity(t *testing.T) {
	want := map[EdgeType]string{
		Aliases:                            "ALIASES",
		AllowsEgress:                       "ALLOWS_EGRESS",
		AllowsIngress:                      "ALLOWS_INGRESS",
		AtlantisDependsOn:                  "ATLANTIS_DEPENDS_ON",
		Calls:                              "CALLS",
		CanAssume:                          "CAN_ASSUME",
		CanEscalateTo:                      "CAN_ESCALATE_TO",
		CanPerform:                         "CAN_PERFORM",
		Contains:                           "CONTAINS",
		CorrelatesDeployableUnit:           "CORRELATES_DEPLOYABLE_UNIT",
		DeclaresCodeowner:                  "DECLARES_CODEOWNER",
		DeclaresDependency:                 "DECLARES_DEPENDENCY",
		Defines:                            "DEFINES",
		DefinesJob:                         "DEFINES_JOB",
		DependsOn:                          "DEPENDS_ON",
		DependsOnPackage:                   "DEPENDS_ON_PACKAGE",
		DeploymentSource:                   "DEPLOYMENT_SOURCE",
		DeploysFrom:                        "DEPLOYS_FROM",
		DiscoversConfigIn:                  "DISCOVERS_CONFIG_IN",
		Documents:                          "DOCUMENTS",
		EvidencesRepositoryRelationship:    "EVIDENCES_REPOSITORY_RELATIONSHIP",
		Executes:                           "EXECUTES",
		ExecutesShell:                      "EXECUTES_SHELL",
		Explains:                           "EXPLAINS",
		ExposesEndpoint:                    "EXPOSES_ENDPOINT",
		ExtendsBase:                        "EXTENDS_BASE",
		GrantsAccessTo:                     "GRANTS_ACCESS_TO",
		HandlesRoute:                       "HANDLES_ROUTE",
		HasAppliedRouting:                  "HAS_APPLIED_ROUTING",
		HasColumn:                          "HAS_COLUMN",
		HasDeploymentEvidence:              "HAS_DEPLOYMENT_EVIDENCE",
		HasIntendedRouting:                 "HAS_INTENDED_ROUTING",
		HasLiveRouting:                     "HAS_LIVE_ROUTING",
		HasParameter:                       "HAS_PARAMETER",
		HasRole:                            "HAS_ROLE",
		HasTaintEvidence:                   "HAS_TAINT_EVIDENCE",
		HasVersion:                         "HAS_VERSION",
		HelmValueReference:                 "HELM_VALUE_REFERENCE",
		Implements:                         "IMPLEMENTS",
		Imports:                            "IMPORTS",
		Indexes:                            "INDEXES",
		Inherits:                           "INHERITS",
		InstanceOf:                         "INSTANCE_OF",
		Instantiates:                       "INSTANTIATES",
		InvokesCloudAction:                 "INVOKES_CLOUD_ACTION",
		LogsTo:                             "LOGS_TO",
		Manages:                            "MANAGES",
		MapsToTable:                        "MAPS_TO_TABLE",
		MatchesState:                       "MATCHES_STATE",
		Migrates:                           "MIGRATES",
		Needs:                              "NEEDS",
		Overrides:                          "OVERRIDES",
		PinsSubmodule:                      "PINS_SUBMODULE",
		ProvisionsDependencyFor:            "PROVISIONS_DEPENDENCY_FOR",
		ProvisionsPlatform:                 "PROVISIONS_PLATFORM",
		QueriesTable:                       "QUERIES_TABLE",
		ReadsConfigFrom:                    "READS_CONFIG_FROM",
		ReadsFrom:                          "READS_FROM",
		ReconcilesFrom:                     "RECONCILES_FROM",
		References:                         "REFERENCES",
		ReferencesTable:                    "REFERENCES_TABLE",
		RepoContains:                       "REPO_CONTAINS",
		RunsImage:                          "RUNS_IMAGE",
		RunsIn:                             "RUNS_IN",
		RunsOn:                             "RUNS_ON",
		SatisfiedBy:                        "SATISFIED_BY",
		SecretsIamAssumesIamRole:           "SECRETS_IAM_ASSUMES_IAM_ROLE",
		SecretsIamAuthenticatesToVaultRole: "SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE",
		SecretsIamGrantsSecretRead:         "SECRETS_IAM_GRANTS_SECRET_READ",
		SecretsIamUsesServiceAccount:       "SECRETS_IAM_USES_SERVICE_ACCOUNT",
		SecretsIamUsesVaultPolicy:          "SECRETS_IAM_USES_VAULT_POLICY",
		TaintFlowsTo:                       "TAINT_FLOWS_TO",
		TargetsEnvironment:                 "TARGETS_ENVIRONMENT",
		To:                                 "TO",
		Triggers:                           "TRIGGERS",
		WritesTo:                           "WRITES_TO",
		TriggersOn:                         "TRIGGERS_ON",
		Uses:                               "USES",
		UsesMetaclass:                      "USES_METACLASS",
		UsesModule:                         "USES_MODULE",
		UsesProfile:                        "USES_PROFILE",
		UsesWorkflow:                       "USES_WORKFLOW",
	}

	for got, str := range want {
		if string(got) != str {
			t.Errorf("edge type constant = %q, want %q", string(got), str)
		}
	}

	if len(want) != len(registered) {
		t.Errorf("parity table has %d entries, registry has %d; keep them in lockstep", len(want), len(registered))
	}
	for _, e := range registered {
		if _, ok := want[e]; !ok {
			t.Errorf("registered edge type %q is missing from the parity table", string(e))
		}
	}
}

// TestRegistryNoDuplicates guards against the same edge-type string appearing
// twice in the registry, which would mask a missing or misnamed constant.
func TestRegistryNoDuplicates(t *testing.T) {
	seen := make(map[EdgeType]struct{}, len(registered))
	for _, e := range registered {
		if _, dup := seen[e]; dup {
			t.Errorf("duplicate edge type in registry: %q", string(e))
		}
		seen[e] = struct{}{}
	}
}

// TestIsRegistered verifies membership behavior for known and unknown strings.
func TestIsRegistered(t *testing.T) {
	if !IsRegistered("DEPENDS_ON") {
		t.Error("IsRegistered(DEPENDS_ON) = false, want true")
	}
	if IsRegistered("NOT_A_REAL_EDGE_TYPE") {
		t.Error("IsRegistered(NOT_A_REAL_EDGE_TYPE) = true, want false")
	}
	if IsRegistered("") {
		t.Error("IsRegistered(\"\") = true, want false")
	}
}

// TestAllReturnsCopy ensures All cannot be used to mutate the backing slice.
func TestAllReturnsCopy(t *testing.T) {
	a := All()
	if len(a) != len(registered) {
		t.Fatalf("All() len = %d, want %d", len(a), len(registered))
	}
	if len(a) > 0 {
		a[0] = "MUTATED"
		if registered[0] == "MUTATED" {
			t.Error("All() exposed the backing registered slice")
		}
	}
}
