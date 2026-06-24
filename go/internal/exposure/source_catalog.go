// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exposure

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// SourceKind names a category in the closed, curated taint-source catalog. A
// taint source is an untrusted-input entry point: the boundary where input the
// program does not control enters. Sources are promoted from the parser's
// existing dead-code root/handler detection (the dead_code_root_kinds tokens),
// which is why each spec lists the root-kind tokens it classifies rather than
// re-detecting handlers.
type SourceKind string

const (
	// SourceHTTPHandler is an HTTP/route handler (the highest-value source: it
	// can be exposed directly to the public internet).
	SourceHTTPHandler SourceKind = "http_handler"
	// SourceRPCHandler is a non-HTTP RPC/service handler.
	SourceRPCHandler SourceKind = "rpc_handler"
	// SourceLambdaHandler is a serverless function handler (often fronted by an
	// API gateway, so internet-exposable).
	SourceLambdaHandler SourceKind = "lambda_handler"
	// SourceMessageConsumer is a queue/topic message consumer (untrusted payload,
	// but not directly internet-facing).
	SourceMessageConsumer SourceKind = "message_consumer"
	// SourceCLICommand is a CLI command entry point (untrusted argv, not
	// internet-facing).
	SourceCLICommand SourceKind = "cli_command"
)

// ExposureRank is the honest, conservative exposure ranking of a source. It is
// derived by combining a source's InternetExposable capability with whether its
// endpoint provably reaches the public internet (the boolean the tracer computes
// from reducer/security_group_reachability.go at trace time).
type ExposureRank string

const (
	// ExposureInternetExposed is an internet-exposable source whose endpoint
	// provably reaches 0.0.0.0/0 (or ::/0) via the security-group graph.
	ExposureInternetExposed ExposureRank = "internet_exposed"
	// ExposureNetworkReachable is an internet-exposable source whose internet
	// reachability is NOT proven (e.g. it may sit behind a private load
	// balancer). It is not over-claimed as internet-exposed.
	ExposureNetworkReachable ExposureRank = "network_reachable"
	// ExposureInternal is a source that is not network-exposable at all (a CLI
	// command, a queue consumer); reachability to the internet is irrelevant.
	ExposureInternal ExposureRank = "internal"
)

// SourceSpec is one curated taint-source classification rule: the source kind, the
// parser dead_code_root_kind tokens that classify a function as that kind,
// whether the kind can be exposed to the public internet, the baseline exposure
// severity, and a provenance citation. The catalog is conservative and closed:
// only well-known untrusted-input entry points are sources. Entrypoints, public
// API, lifecycle callbacks, tests, and generated code are deliberately excluded —
// untrusted input does not enter there.
type SourceSpec struct {
	// Kind is the closed-vocabulary source category.
	Kind SourceKind
	// DisplayName is the human-facing label for surfaces and findings.
	DisplayName string
	// RootKinds are the parser dead_code_root_kind tokens that classify a function
	// as this source kind. A token maps to exactly one source kind.
	RootKinds []string
	// InternetExposable is true when this source kind can be reached directly from
	// the public internet (HTTP/RPC/Lambda handlers). CLI commands and message
	// consumers are not.
	InternetExposable bool
	// BaselineSeverity is the exposure severity this source contributes before
	// path-specific aggravation.
	BaselineSeverity Severity
	// Provenance cites where the root-kind tokens are authored (the dead-code root
	// catalog), keeping the classification auditable.
	Provenance string
}

// deadCodeRootCatalogProvenance cites the authoritative root-kind catalog the
// taint-source tokens are drawn from, so the classification stays auditable
// against the dead-code analysis surface.
const deadCodeRootCatalogProvenance = "query/code_dead_code_analysis.go modeled_framework_roots / modeled_entrypoints (parser dead_code_root_kinds)"

// sourceCatalog is the curated, closed set of taint-source classification rules.
// The root-kind tokens are a conservative subset of the dead-code root catalog:
// only genuine untrusted-input entry points across the supported languages. It is
// ordered so internet-exposable kinds precede non-exposable ones; ClassifySource
// relies on that order to prefer the higher-value source when a function carries
// multiple tokens. It is a package-level value built once; callers MUST NOT
// mutate it.
var sourceCatalog = []SourceSpec{
	{
		Kind:        SourceHTTPHandler,
		DisplayName: "HTTP/route handler",
		RootKinds: []string{
			"go.net_http_handler_signature",
			"go.net_http_handler_registration",
			"python.fastapi_route_decorator",
			"python.flask_route_decorator",
			"javascript.express_route_registration",
			"javascript.koa_route_registration",
			"javascript.fastify_route_registration",
			"javascript.nestjs_controller_method",
			"javascript.nextjs_route_export",
			"javascript.nextjs_app_export",
			"javascript.hapi_route_config_handler",
			"javascript.hapi_handler_export",
			"java.spring_request_mapping_method",
			"kotlin.spring_request_mapping_method",
			"csharp.aspnet_controller_action",
			"ruby.rails_controller_action",
			"php.route_handler",
			"php.framework_controller_action",
			"php.symfony_route_attribute",
			"elixir.phoenix_controller_action",
			"scala.play_controller_action",
			"swift.vapor_route_handler",
		},
		InternetExposable: true,
		BaselineSeverity:  SeverityHigh,
		Provenance:        deadCodeRootCatalogProvenance,
	},
	{
		Kind:        SourceRPCHandler,
		DisplayName: "RPC / web-service / WebSocket handler",
		// Stapler is Jenkins' HTTP-dispatch web method; Phoenix LiveView callbacks
		// run over a WebSocket. Both accept untrusted input over a network socket,
		// so they are internet-exposable like an HTTP handler.
		RootKinds: []string{
			"java.stapler_web_method",
			"elixir.phoenix_liveview_callback",
		},
		InternetExposable: true,
		BaselineSeverity:  SeverityHigh,
		Provenance:        deadCodeRootCatalogProvenance,
	},
	{
		Kind:        SourceLambdaHandler,
		DisplayName: "Serverless function handler",
		RootKinds: []string{
			"python.aws_lambda_handler",
		},
		InternetExposable: true,
		BaselineSeverity:  SeverityHigh,
		Provenance:        deadCodeRootCatalogProvenance,
	},
	{
		Kind:        SourceMessageConsumer,
		DisplayName: "Message/queue/actor consumer",
		// Celery and AMQP consume external broker payloads; an Akka actor receive
		// consumes intra-system actor messages. All receive input the function does
		// not control, but none is directly internet-facing.
		RootKinds: []string{
			"python.celery_task_decorator",
			"javascript.hapi_amqp_consumer",
			"scala.akka_actor_receive",
		},
		InternetExposable: false,
		BaselineSeverity:  SeverityMedium,
		Provenance:        deadCodeRootCatalogProvenance,
	},
	{
		Kind:        SourceCLICommand,
		DisplayName: "CLI command",
		RootKinds: []string{
			"go.cobra_run_signature",
			"go.cobra_run_registration",
			"python.click_command_decorator",
			"python.typer_command_decorator",
			"elixir.mix_task_run",
		},
		InternetExposable: false,
		BaselineSeverity:  SeverityLow,
		Provenance:        deadCodeRootCatalogProvenance,
	},
}

// sourceRootKindIndex maps each curated root-kind token to its source spec for
// O(1) classification. It is built once from sourceCatalog at package init; the
// well-formedness test guarantees no token maps to two kinds.
var sourceRootKindIndex = buildSourceRootKindIndex()

func buildSourceRootKindIndex() map[string]SourceSpec {
	index := make(map[string]SourceSpec)
	for _, spec := range sourceCatalog {
		for _, rk := range spec.RootKinds {
			index[rk] = spec
		}
	}
	return index
}

// SourceCatalog returns a defensive copy of the curated taint-source catalog,
// deep-copying each spec's RootKinds so callers cannot mutate the package-level
// value in place.
func SourceCatalog() []SourceSpec {
	out := make([]SourceSpec, len(sourceCatalog))
	copy(out, sourceCatalog)
	for i := range out {
		out[i].RootKinds = cloneStrings(out[i].RootKinds)
	}
	return out
}

// cloneStrings returns a deep copy of a string slice (nil for empty).
func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ClassifySource returns the taint-source spec for a function carrying the given
// parser dead_code_root_kind tokens. When a function carries tokens for more than
// one source kind, the catalog-order winner is returned (internet-exposable kinds
// are ordered first, so the higher-value source wins). ok is false when no token
// classifies the function as a source — entrypoints, public API, tests, and
// generated code are intentionally not sources.
func ClassifySource(rootKinds []string) (SourceSpec, bool) {
	var best SourceSpec
	// noMatch is the "nothing classified yet" sentinel: a rank no real catalog
	// index can equal, so the first matching token always wins and later tokens
	// win only if they sit earlier (higher priority) in the catalog.
	const noMatch = -1
	bestRank := noMatch
	for _, rk := range rootKinds {
		spec, ok := sourceRootKindIndex[rk]
		if !ok {
			continue
		}
		rank := sourceCatalogOrder(spec.Kind)
		if bestRank == noMatch || rank < bestRank {
			best = spec
			bestRank = rank
		}
	}
	if bestRank == noMatch {
		return SourceSpec{}, false
	}
	best.RootKinds = cloneStrings(best.RootKinds)
	return best, true
}

// sourceCatalogOrder returns the index of a source kind in the catalog; a lower
// index is higher priority (internet-exposable kinds are ordered first). An
// unknown kind sorts last. The index is always >= 0, never the ClassifySource
// no-match sentinel.
func sourceCatalogOrder(kind SourceKind) int {
	for i, spec := range sourceCatalog {
		if spec.Kind == kind {
			return i
		}
	}
	return len(sourceCatalog)
}

// RankSourceExposure ranks a source's exposure honestly. A non-internet-exposable
// source is always internal. An internet-exposable source is internet_exposed
// only when endpointReachesInternet is true (the tracer derives this from the
// security-group graph: an endpoint reachable from a CidrBlock{is_internet:true}).
// An internet-exposable source without proven reachability is network_reachable,
// never over-claimed as internet-exposed.
func RankSourceExposure(spec SourceSpec, endpointReachesInternet bool) ExposureRank {
	if !spec.InternetExposable {
		return ExposureInternal
	}
	if endpointReachesInternet {
		return ExposureInternetExposed
	}
	return ExposureNetworkReachable
}

// sourceCatalogVersionGolden pins the current content hash of the taint-source
// catalog. The well-formedness/version test fails when the catalog changes
// without a deliberate update, the taintModelVersion discipline.
const sourceCatalogVersionGolden = "74deeb6ae003577c9441e75b407ee970fc445e204324dce4c03abffd8c98b12f"

// SourceCatalogVersion returns a deterministic content hash over the curated
// taint-source catalog so cached reachability findings can be invalidated when
// the catalog changes. It is stable across process runs and order-independent.
func SourceCatalogVersion() string {
	return hashSourceSpecs(sourceCatalog)
}

// hashSourceSpecs computes the canonical content hash of a source-spec slice.
// Each spec is serialized field-by-field (root-kinds sorted) into a stable line
// PREFIXED with its catalog index, the lines are sorted, and the joined text is
// SHA-256 hashed. The index prefix encodes catalog ORDER into the hash: because
// ClassifySource resolves a multi-kind function to the earliest-ordered matching
// kind, a reordering that changes classification priority must invalidate cached
// reachability findings. Sorting after prefixing keeps the hash deterministic
// while any field change — or any order change — still changes it.
func hashSourceSpecs(specs []SourceSpec) string {
	lines := make([]string, 0, len(specs))
	for i, spec := range specs {
		roots := cloneStrings(spec.RootKinds)
		sort.Strings(roots)
		lines = append(lines, strings.Join([]string{
			strconv.Itoa(i),
			string(spec.Kind),
			spec.DisplayName,
			strings.Join(roots, "\x1d"),
			boolToken(spec.InternetExposable),
			string(spec.BaselineSeverity),
			spec.Provenance,
		}, "\x1f"))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\x1e")))
	return hex.EncodeToString(sum[:])
}

// boolToken renders a bool as a stable token for content hashing.
func boolToken(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
