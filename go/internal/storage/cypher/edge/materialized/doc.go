// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package materialized owns the materialized-edge family registries that the
// Ifá exhaustiveness gates read: single-type families (codeowners,
// submodule-pin, invokes-cloud-action, handles-route, runs-in, shell-exec,
// deployable-unit, documentation, code-call, inheritance, plus the nine
// DIRECT-materialization families and the node-scoped code_taint_evidence
// family) expose their edge types, endpoint labels, and identity properties;
// the repo-dependency multi-type family exposes its DEPENDS_ON alternation
// plus the split retract builder the live retract path executes; and the
// multi-type DIRECT families (incident_routing_evidence,
// observability_coverage, security_group_sg_rule in direct_families.go) expose
// their closed vocabularies with a RetractKind (relAlternation or
// evidenceNodeDetach) plus every production retract variant the drift guard
// pins (RetractCypher plus RetractVariants). The one predicate-scoped
// family (cloud_resource, #8358) exposes no type set — its writers prefix
// an open verb vocabulary — but the (type-prefix, evidence_source,
// endpoint-label) predicate its scan scopes by.
//
// The registries are data, not writers: every Cypher template they cite is
// owned by the edge/writer sibling or the canonical writers in the cypher
// root, and the reason strings name the exact template or retract constant
// that writes or reaps each type. This package imports the parent cypher
// package and the edge/writer sibling; it must not be imported by the
// parent cypher package.
package materialized
