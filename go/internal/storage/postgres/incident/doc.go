// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package incidentstore reads the applied PagerDuty service-routing facts the
// incident-repository correlation reducer domain (#2161) needs, and adapts the
// tfstatebackend resolver to that reducer's port shape.
//
// PostgresAppliedPagerDutyServiceRoutingLoader.LoadAppliedPagerDutyServiceRouting
// runs ListAppliedPagerDutyServiceRoutingQuery, which is scoped to
// resource_class='service' rows of the applied_pagerduty_resource fact kind so
// every returned row carries the real PagerDuty provider service id and the
// Terraform backend locator an edge can anchor on. Rows without a provider id
// are still returned with a blank ProviderObjectID rather than dropped, so the
// pure correlation builder can record them as provenance-only rejected
// decisions instead of the loader silently hiding partial coverage.
//
// BackendRepositoryResolverAdapter.ResolveBackendRepository bridges
// tfstatebackend.Resolver's sentinel errors to the reducer's data-only
// BackendRepositoryResolution: no owning repo becomes a blank resolution, an
// ambiguous owner sets Ambiguous=true, a single owner returns its repository
// id, and any other error propagates.
//
// The query text is a permanent raw-SQL decision (#4683): its
// resource_class='service' predicate and provider_object_id ordering must stay
// in the query text verbatim so the partial expression index
// fact_records_incident_routing_applied_service_idx stays eligible; decoding
// the predicate in Go instead would force a full sequential scan. This
// package must not import the parent postgres package.
package incidentstore
