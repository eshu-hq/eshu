// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"aws_relationship", "aws_resource"},
})

// AzureHandleBinding declares this package's verified reducer consumption of
// azure kinds (issue #6629). Build-time metadata only.
var AzureHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_azure", FamilyID: "azure",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"azure_cloud_relationship", "azure_cloud_resource"},
})

// CodeBuildCodeImportRepoDependencyIntentsBinding declares this package's verified reducer consumption of
// code kinds (issue #6629). Build-time metadata only.
var CodeBuildCodeImportRepoDependencyIntentsBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_build_code_import_repo_dependency_intents", FamilyID: "code",
	ReducerDir: "go/internal/reducer", Symbol: "BuildCodeImportRepoDependencyIntents",
	Kinds: []string{"file", "repository"},
})

// DocumentationHandleBinding declares this package's verified reducer consumption of
// documentation kinds (issue #6629). Build-time metadata only.
var DocumentationHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_documentation", FamilyID: "documentation",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"documentation_document", "documentation_entity_mention"},
})

// GCPHandleBinding declares this package's verified reducer consumption of
// gcp kinds (issue #6629). Build-time metadata only.
var GCPHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_gcp", FamilyID: "gcp",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"gcp_cloud_relationship", "gcp_cloud_resource"},
})

// KubernetesLiveHandleBinding declares this package's verified reducer consumption of
// kubernetes_live kinds (issue #6629). Build-time metadata only.
var KubernetesLiveHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_kubernetes_live", FamilyID: "kubernetes_live",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"kubernetes_live.namespace", "kubernetes_live.pod_template"},
})

// ReducerDerivedSharedProjectionRowRepoIDBinding declares this package's verified reducer consumption of
// reducer_derived kinds (issue #6629). Build-time metadata only. The anchor
// was the shared-projection row-repo helper; once that helper retired to
// [sharedintent.RowRepoID] (#6626 tail bucket 2) the anchor moved to the
// owner-index builder that consumes both decision sets.
var ReducerDerivedSharedProjectionRowRepoIDBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_shared_projection_row_repo_i_d", FamilyID: "reducer_derived",
	ReducerDir: "go/internal/reducer", Symbol: "buildCodeImportOwnerIndex",
	Kinds: []string{"reducer_package_ownership_correlation", "reducer_package_publication_correlation"},
})

// SubmoduleHandleBinding declares this package's verified reducer consumption of
// submodule kinds (issue #6629). Build-time metadata only.
var SubmoduleHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_submodule", FamilyID: "submodule",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"submodule.pin"},
})

// ECSBindingHandleBinding declares this package's verified reducer
// consumption of the applied-state kind for workload↔ECS-service binding
// (issue #9661, mechanism 1b phase-2 scaffold). The workload-cloud
// handler reads terraform_state_resource facts naming managed type
// aws_ecs_service; producer coverage is zero, so the path is proven by a
// synthetic envelope until the producer ships. Build-time metadata only.
var ECSBindingHandleBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_handle_ecs_binding", FamilyID: "terraform_state",
	ReducerDir: "go/internal/reducer", Symbol: "Handle",
	Kinds: []string{"terraform_state_resource"},
})
