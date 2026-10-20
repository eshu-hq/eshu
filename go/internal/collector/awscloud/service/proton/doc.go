// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package proton maps AWS Proton control-plane metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for Proton environments,
// services, environment templates, and service templates, plus relationships
// for service-in-environment (derived from service instances) and
// environment-uses-IAM-role evidence. Service and environment spec manifest
// bodies, pipeline spec bodies, template version schema bodies, and any
// deployment input parameter values stay outside this package contract: the
// scanner is metadata-only and never mutates Proton state.
package proton
