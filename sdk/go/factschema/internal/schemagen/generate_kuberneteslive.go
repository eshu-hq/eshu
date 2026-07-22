// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// KubernetesLivePodTemplateSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.pod_template" payload.
const KubernetesLivePodTemplateSchemaID = schemaBaseID + "kuberneteslive/v1/pod_template.schema.json"

// KubernetesLivePodTemplateSchema returns the JSON Schema bytes for
// kuberneteslivev1.PodTemplate.
func KubernetesLivePodTemplateSchema() ([]byte, error) {
	return reflectSchema(KubernetesLivePodTemplateSchemaID, "Eshu kubernetes_live.pod_template Payload (schema version 1)", &kuberneteslivev1.PodTemplate{})
}

// KubernetesLiveRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.relationship" payload.
const KubernetesLiveRelationshipSchemaID = schemaBaseID + "kuberneteslive/v1/relationship.schema.json"

// KubernetesLiveRelationshipSchema returns the JSON Schema bytes for
// kuberneteslivev1.Relationship.
func KubernetesLiveRelationshipSchema() ([]byte, error) {
	return reflectSchema(KubernetesLiveRelationshipSchemaID, "Eshu kubernetes_live.relationship Payload (schema version 1)", &kuberneteslivev1.Relationship{})
}

// KubernetesLiveWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.warning" payload.
const KubernetesLiveWarningSchemaID = schemaBaseID + "kuberneteslive/v1/warning.schema.json"

// KubernetesLiveWarningSchema returns the JSON Schema bytes for
// kuberneteslivev1.Warning.
func KubernetesLiveWarningSchema() ([]byte, error) {
	return reflectSchema(KubernetesLiveWarningSchemaID, "Eshu kubernetes_live.warning Payload (schema version 1)", &kuberneteslivev1.Warning{})
}

// KubernetesLiveNamespaceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "kubernetes_live.namespace" payload.
const KubernetesLiveNamespaceSchemaID = schemaBaseID + "kuberneteslive/v1/namespace.schema.json"

// KubernetesLiveNamespaceSchema returns the JSON Schema bytes for
// kuberneteslivev1.Namespace.
func KubernetesLiveNamespaceSchema() ([]byte, error) {
	return reflectSchema(KubernetesLiveNamespaceSchemaID, "Eshu kubernetes_live.namespace Payload (schema version 1)", &kuberneteslivev1.Namespace{})
}
