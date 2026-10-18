// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetescorrelation

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// KubernetesLiveHandleBinding declares this package's verified reducer consumption of
// kubernetes_live kinds (issue #6629). Build-time metadata only.
var KubernetesLiveHandleBinding = contract.Bind(contract.Binding{
	ID: "kubernetescorrelation/kubernetescorrelation_handle", FamilyID: "kubernetes_live",
	ReducerDir: "go/internal/reducer/kubernetescorrelation", Symbol: "Handle",
	Kinds: []string{"kubernetes_live.pod_template", "kubernetes_live.relationship", "kubernetes_live.warning"},
})

// OCIRegistryBuildSourceImageDigestJoinIndexBinding declares this package's verified reducer consumption of
// oci_registry kinds (issue #6629). Build-time metadata only.
var OCIRegistryBuildSourceImageDigestJoinIndexBinding = contract.Bind(contract.Binding{
	ID: "kubernetescorrelation/kubernetescorrelation_build_source_image_digest_join_index", FamilyID: "oci_registry",
	ReducerDir: "go/internal/reducer/kubernetescorrelation", Symbol: "BuildSourceImageDigestJoinIndex",
	Kinds: []string{"oci_registry.image_descriptor", "oci_registry.image_index", "oci_registry.image_manifest", "oci_registry.image_tag_observation"},
})
