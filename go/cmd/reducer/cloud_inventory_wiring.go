// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// cloudInventoryAdmissionWiring builds the production adapters for the shared
// multi-cloud inventory admission domain (issues #1997, #1998).
//
// The evidence loader reads the three provider inventory source fact kinds
// (aws_resource, gcp_cloud_resource, azure_cloud_resource) for one scope
// generation and maps each into the shared admission record shape. The writer
// upserts one canonical reducer_cloud_resource_identity row per resolved
// cloud_resource_uid, idempotent by a deterministic fact id so retries and
// concurrent workers converge instead of duplicating canonical truth. The
// generation check supersedes a stale scan before any load or write so a
// superseded admission never publishes canonical rows.
// Resource-change evidence is wired as additive freshness only: it decorates an
// already-admitted canonical resource and cannot create resources or finalize
// tombstones.
//
// The adapters are returned together because the reducer registry only registers
// DomainCloudInventoryAdmission when both the loader and writer are non-nil;
// keeping the wiring in one helper keeps that contract obvious and keeps the
// reducer command entrypoint under the package size budget.
func cloudInventoryAdmissionWiring(
	database postgres.ExecQueryer,
	logger *slog.Logger,
) (
	reducer.CloudInventoryEvidenceLoader,
	reducer.CloudInventoryAdmissionWriter,
	reducer.GenerationFreshnessCheck,
	reducer.CloudTagEvidenceLoader,
	reducer.CloudIdentityPolicyEvidenceLoader,
	reducer.CloudResourceChangeEvidenceLoader,
) {
	loader := postgres.PostgresCloudInventoryEvidenceLoader{
		DB:     database,
		Logger: logger,
	}
	writer := reducer.PostgresCloudInventoryAdmissionWriter{DB: database}
	generationCheck := postgres.NewGenerationFreshnessCheck(database)
	// Tag-evidence loader attaches azure_tag_observation fingerprints onto the
	// canonical resource sharing their uid (#2192). It is additive: a nil loader
	// would leave the AWS/GCP resource path unchanged.
	tagEvidenceLoader := postgres.PostgresCloudTagEvidenceLoader{
		DB:     database,
		Logger: logger,
	}
	identityPolicyEvidenceLoader := postgres.PostgresCloudIdentityPolicyEvidenceLoader{
		DB:     database,
		Logger: logger,
	}
	resourceChangeEvidenceLoader := postgres.PostgresCloudResourceChangeEvidenceLoader{
		DB:     database,
		Logger: logger,
	}
	return loader, writer, generationCheck, tagEvidenceLoader, identityPolicyEvidenceLoader, resourceChangeEvidenceLoader
}
