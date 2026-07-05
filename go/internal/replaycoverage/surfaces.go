// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// Registry identifies which source-of-truth registry a required surface is
// enumerated from. The coverage gate proves every surface across these
// registries has a green replay scenario.
type Registry string

const (
	// ParserSurfacePrefix is the canonical coverage-key prefix for parser-backed
	// surfaces declared in the manifest.
	ParserSurfacePrefix = "parser:"

	// RegistrySurfaceInventory is specs/surface-inventory.v1.yaml: the
	// implemented-lane collectors are the required cassette-replay targets.
	RegistrySurfaceInventory Registry = "surface_inventory"
	// RegistryFactKind is specs/fact-kind-registry.v1.yaml: each distinct
	// read_surface is a required API/MCP golden-replay target.
	RegistryFactKind Registry = "fact_kind_registry"
	// RegistryCLIReadSurface is the B-12 snapshot query_shapes.cli catalog: each
	// CLI read command is a required CLI golden-replay target.
	RegistryCLIReadSurface Registry = "cli_read_surface"
	// RegistryParserLedger is specs/parser-backing-ledger.v1.yaml: each parser is
	// a required parser-fixture-replay target.
	RegistryParserLedger Registry = "parser_backing_ledger"
	// RegistryCapabilityMatrix is specs/capability-matrix.v1.yaml: each positively
	// claimed capability is a required claim-or-refusal-replay target.
	RegistryCapabilityMatrix Registry = "capability_matrix"
	// RegistryProductClaims is specs/product-claims.v1.yaml: each public product
	// claim is a required deterministic proof-ledger replay target.
	RegistryProductClaims Registry = "product_claim_ledger"
	// RegistryAuthorizationCatalog is specs/authorization-catalog.v1.yaml: each
	// live permission family must prove in-grant and out-of-grant scoped reads.
	RegistryAuthorizationCatalog Registry = "authorization_catalog"
)

// allRegistries is the closed, ordered set of coverage registries.
var allRegistries = []Registry{
	RegistrySurfaceInventory,
	RegistryFactKind,
	RegistryCLIReadSurface,
	RegistryParserLedger,
	RegistryCapabilityMatrix,
	RegistryProductClaims,
	RegistryAuthorizationCatalog,
}

// AllRegistries returns every coverage registry in a stable order.
func AllRegistries() []Registry {
	return append([]Registry(nil), allRegistries...)
}

// SupportedSurface is one surface Eshu claims to support that must be backed by a
// green replay scenario. Key is the canonical, registry-qualified identifier the
// coverage manifest maps to a scenario (e.g. "collector:aws", "parser:hcl").
type SupportedSurface struct {
	// Registry is the source-of-truth registry the surface came from.
	Registry Registry
	// Key is the canonical "<kind>:<name>" coverage key the manifest maps.
	Key string
	// Detail is a short human description for the coverage report.
	Detail string
}

// EnumerateSupported reconciles the source-of-truth registries into the flat,
// deterministic set of surfaces that must each have a green replay scenario:
//
//   - surface-inventory: collectors on the implemented readiness lane (only that
//     lane asserts production readiness), keyed "collector:<name>";
//   - fact-kind registry: each distinct non-blank read_surface, keyed
//     "read_surface:<surface>";
//   - B-12 CLI query shapes: each distinct non-blank CLI read surface, keyed
//     "cli_surface:<command>";
//   - parser-backing ledger: each parser, keyed "parser:<name>";
//   - capability matrix: each capability with at least one positively-claimed
//     profile, keyed "capability:<id>";
//   - product claim ledger: each broad public product claim, keyed
//     "product_claim:<id>";
//   - authorization catalog: each live permission family with capability
//     prefixes contributes "authz_family:<family>:in_grant" and
//     "authz_family:<family>:out_of_grant".
//
// The result is sorted by registry then key so gate output and the coverage
// report are byte-stable across runs.
func EnumerateSupported(
	inv capabilitycatalog.SurfaceInventory,
	factKinds []facts.FactKindRegistryEntry,
	ledger ParserLedger,
	matrix capabilitycatalog.Matrix,
	productClaims capabilitycatalog.ProductClaimLedger,
	cliShapes map[string]goldengate.QueryShape,
	authorization capabilitycatalog.AuthorizationCatalog,
) []SupportedSurface {
	var out []SupportedSurface

	// surface-inventory contributes its collectors: collectors are the surface the
	// replay chain (design §2) starts from, and a cassette is their scenario. The
	// implemented-lane api_route and mcp_tool surfaces are the read side and are
	// covered through the fact-kind read_surface enumeration below; full API/MCP
	// surface coverage (including any route with no fact-kind read_surface) is C-5's
	// (#4177) scope, deliberately not double-counted here.
	for _, rec := range inv.Surfaces {
		if rec.Category != capabilitycatalog.SurfaceCollector {
			continue
		}
		if rec.Readiness != capabilitycatalog.ReadinessImplemented {
			continue
		}
		out = append(out, SupportedSurface{
			Registry: RegistrySurfaceInventory,
			Key:      "collector:" + rec.Name,
			Detail:   fmt.Sprintf("implemented-lane collector %q", rec.Name),
		})
	}

	readSurfaceFamilies := map[string]int{}
	for _, entry := range factKinds {
		rs := strings.TrimSpace(entry.ReadSurface)
		if rs == "" {
			continue
		}
		readSurfaceFamilies[rs]++
	}
	for rs, n := range readSurfaceFamilies {
		out = append(out, SupportedSurface{
			Registry: RegistryFactKind,
			Key:      "read_surface:" + rs,
			Detail:   fmt.Sprintf("read surface %q (%d fact kind(s))", rs, n),
		})
	}

	for key := range cliShapes {
		command := strings.TrimSpace(key)
		if command == "" {
			continue
		}
		out = append(out, SupportedSurface{
			Registry: RegistryCLIReadSurface,
			Key:      "cli_surface:" + command,
			Detail:   fmt.Sprintf("CLI read surface %q", command),
		})
	}

	for _, p := range ledger.Parsers {
		out = append(out, SupportedSurface{
			Registry: RegistryParserLedger,
			Key:      ParserSurfacePrefix + p.Parser,
			Detail:   fmt.Sprintf("parser %q", p.Parser),
		})
	}

	for _, capRow := range matrix.Capabilities {
		if !hasPositiveClaim(capRow) {
			continue
		}
		out = append(out, SupportedSurface{
			Registry: RegistryCapabilityMatrix,
			Key:      "capability:" + capRow.Capability,
			Detail:   fmt.Sprintf("capability %q", capRow.Capability),
		})
	}

	for _, claim := range productClaims.Claims {
		id := strings.TrimSpace(claim.ID)
		if id == "" {
			continue
		}
		out = append(out, SupportedSurface{
			Registry: RegistryProductClaims,
			Key:      "product_claim:" + id,
			Detail:   fmt.Sprintf("product claim %q", id),
		})
	}

	for _, family := range liveAuthorizationFamilies(authorization) {
		for _, mode := range authorizationGrantModes {
			out = append(out, SupportedSurface{
				Registry: RegistryAuthorizationCatalog,
				Key:      fmt.Sprintf("authz_family:%s:%s", family.Family, mode),
				Detail:   fmt.Sprintf("authorization family %q %s scoped-route proof", family.Family, mode),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Registry != out[j].Registry {
			return out[i].Registry < out[j].Registry
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// hasPositiveClaim reports whether a capability declares support (supported or
// experimental) in at least one profile, using the catalog's own canonical status
// resolver so a truth-ceiling-only row (blank status, non-unsupported ceiling) is
// correctly counted as the claim it is — and the gate never forks the matrix's
// status vocabulary. A capability whose every profile is unsupported asserts
// nothing to prove and is not a coverage target.
func hasPositiveClaim(capRow capabilitycatalog.MatrixCapability) bool {
	for _, profile := range capRow.Profiles {
		if capabilitycatalog.ProfileClaimsSupport(profile) {
			return true
		}
	}
	return false
}

var authorizationGrantModes = []string{"in_grant", "out_of_grant"}

func liveAuthorizationFamilies(catalog capabilitycatalog.AuthorizationCatalog) []capabilitycatalog.PermissionFamily {
	var families []capabilitycatalog.PermissionFamily
	seen := map[string]struct{}{}
	for _, family := range catalog.PermissionFamilies {
		name := strings.TrimSpace(family.Family)
		if name == "" || family.Planned || !hasCapabilityPrefix(family) {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		family.Family = name
		families = append(families, family)
		seen[name] = struct{}{}
	}
	return families
}

func hasCapabilityPrefix(family capabilitycatalog.PermissionFamily) bool {
	for _, prefix := range family.CapabilityPrefixes {
		if strings.TrimSpace(prefix) != "" {
			return true
		}
	}
	return false
}
