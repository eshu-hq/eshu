// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package infrasearchtools defines pure route selection for the MCP
// infrastructure-search family.
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The ecosystem child owns the advertised
// definition. The query package owns the bounded read behind the path, which
// searches cloud, Kubernetes, Terraform, ArgoCD, Crossplane, and Helm resource
// nodes by free text or by structured filters, one indexed label scan per
// category. This package runs no query and must keep the tool name, request
// path, and body keys stable.
//
// The search carries eight body keys: seven scope keys (query, category, kind,
// provider, environment, resource_service, and resource_category), of which
// the handler requires at least one to be non-blank after trimming; and limit,
// which defaults to 50 here. The handler's limit bound is asymmetric and never
// rejects -- it substitutes 50 for any value at or below zero and clamps
// anything above 200 down to 200 -- so 0 and -1 mean a 50-row page and 500
// means a 200-row page, never a 400. category, when set, must be one of k8s,
// terraform, argocd, crossplane, helm, or cloud, or the handler returns 400.
// Dropping a key is not uniformly loud: losing a scope key 400s only the caller
// whose sole scope it was and silently widens everyone else's page, while
// losing limit hands every caller 50 rows with no error at all.
//
// limit honours only a Go number and travels as an int in the JSON body. The
// string "25" falls back to 50, so a client that stringifies the number gets
// a 50-row page rather than an error.
package infrasearchtools
