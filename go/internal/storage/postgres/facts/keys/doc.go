// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factkeys deletes the three per-fact key sidecars
// (relationship_reference_candidate_keys, package_manifest_consumption_keys
// and package_registry_identity_keys) explicitly, so a fact delete does not
// rely on their per-fact foreign key cascade (#7420). That cascade fires one
// trigger per deleted fact per table and dominates a large generation prune.
//
// Generation retention calls DeleteGenerations before it deletes
// scope_generations, and DeletePage before an over-limit chunk page's fact
// delete, in the caller's transaction. Both run the tables in Deletes order
// and key every statement on the (scope_id, generation_id, fact_id) partition
// index of each table. Migrations 188 to 190 dropped the per-fact foreign
// keys, so these deletes are what keep the sidecars free of orphan rows;
// OrphanCensusSQL is the operator census that counts any that remain.
package factkeys
