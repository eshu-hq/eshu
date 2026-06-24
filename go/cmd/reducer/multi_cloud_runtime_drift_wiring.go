// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// resolveNornicDBGroupedWrites reports whether NornicDB semantic grouped canonical
// writes are enabled for the configured graph backend. Grouped writes only apply
// to the NornicDB backend; any other backend is always false. A configuration
// error is surfaced to the caller, and an enabled flag is logged once at startup
// so an operator can confirm conformance mode is active. Extracted from the
// reducer service builder to keep the entrypoint under the package size budget.
func resolveNornicDBGroupedWrites(
	getenv func(string) string,
	graphBackend runtimecfg.GraphBackend,
	logger *slog.Logger,
) (bool, error) {
	if graphBackend != runtimecfg.GraphBackendNornicDB {
		return false, nil
	}
	grouped, err := nornicDBCanonicalGroupedWrites(getenv)
	if err != nil {
		return false, err
	}
	if grouped && logger != nil {
		logger.Warn("NornicDB semantic grouped writes enabled for conformance",
			"graph_backend", string(graphBackend),
			"grouped_writes", true,
			"env_var", nornicDBCanonicalGroupedWritesEnv)
	}
	return grouped, nil
}

// multiCloudRuntimeDriftWiring builds the production adapters for the
// provider-neutral runtime drift publication domain (issues #1997, #1998).
//
// The evidence loader joins observed provider inventory facts (AWS, GCP, Azure)
// for one collector generation against active Terraform-state rows whose
// provider-native identity resolves into the same canonical cloud_resource_uid
// keyspace, then resolves each state backend to its owning config snapshot before
// classifying drift. The writer upserts one canonical
// reducer_multi_cloud_runtime_drift_finding fact per admitted finding, idempotent
// by a deterministic fact id so retries and concurrent workers converge instead
// of duplicating findings.
//
// Both are returned together because the reducer registry only registers
// DomainMultiCloudRuntimeDrift when both the loader and writer are non-nil;
// keeping the wiring in one helper keeps that contract obvious and keeps the
// reducer command entrypoint under the package size budget. The config resolver
// is the same shared tfstatebackend resolver the AWS drift and config/state drift
// domains use so all three agree on backend ownership.
func multiCloudRuntimeDriftWiring(
	database postgres.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (reducer.MultiCloudRuntimeDriftEvidenceLoader, reducer.MultiCloudRuntimeDriftFindingWriter, *slog.Logger) {
	loader := postgres.PostgresMultiCloudRuntimeDriftEvidenceLoader{
		DB: database,
		ConfigResolver: tfstatebackend.NewResolver(
			postgres.PostgresTerraformBackendQuery{DB: database},
		),
		Tracer:      tracer,
		Logger:      logger,
		Instruments: instruments,
	}
	writer := reducer.PostgresMultiCloudRuntimeDriftWriter{DB: database}
	return loader, writer, logger
}
