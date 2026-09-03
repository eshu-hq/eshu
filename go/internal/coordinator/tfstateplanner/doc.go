// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfstateplanner plans workflow rows for exact Terraform-state candidates
// without opening the state source.
//
// WorkPlanner validates one enabled, claim-capable collector instance, parses
// its discovery configuration, resolves candidates through the injected
// terraformstate.GitReadinessChecker and terraformstate.BackendFactReader
// ports, and returns one deterministic workflow run plus one claimable work
// item per candidate. Candidate identity travels as a hashed planning ID, so
// no raw bucket, key, or version locator reaches a run or work-item field.
// The parent coordinator owns scheduling order, the plan-key clock, durable
// open-target admission, the waiting-on-git-generation retry, and telemetry;
// this package resolves no credentials and reads no Terraform state payload.
package tfstateplanner
