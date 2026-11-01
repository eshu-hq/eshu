// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// ownedResolverVirtualSlotCount is V, the virtual-slot modulus of the OSV
	// slot scopes. It mirrors the coordinator's pinned constant; the live
	// differential compares the slot this resolver reports with the slot the
	// production planner mints, so the two cannot drift silently.
	ownedResolverVirtualSlotCount = 1024
	// ownedResolverNameChunk bounds the names one seek statement carries. The
	// reader and the proof shims show a hit list of tens of names per pass; the
	// chunk keeps a pathological batch from becoming one unbounded LATERAL
	// probe. The PyPI and swift reads are one prefix read per call instead.
	ownedResolverNameChunk = 50
)

// OwnedPackageNameLookup names one OSV package to resolve against the owned
// dependency inventory.
type OwnedPackageNameLookup struct {
	// Ecosystem is the owned derivation spelling from
	// workflow.CanonicalOwnedPackageEcosystem, one of
	// workflow.VulnerabilityOwnedDerivationEcosystems: Go is "go", Packagist is
	// "composer", crates.io is "cargo". Reader aliases ("gradle", "gomod")
	// resolve to their canonical ecosystem. Anything else is an error, because
	// an empty answer would read as "nothing owned".
	Ecosystem string
	// Name is the package name as the advisory feed spells it: for swift
	// host/path ("github.com/apple/swift-nio"), a clone URL or a bare package
	// name, for Maven the group:artifact coordinate.
	Name string
}

// OwnedPackageResolvedTarget is one owned (entity_name, version) a lookup
// matched, with the hash and virtual slot the owned-package reader and the
// slot-scope planner assign it.
type OwnedPackageResolvedTarget struct {
	// Lookup is the lookup that matched, canonicalized: Ecosystem holds the
	// owned derivation spelling and Name is trimmed, so a caller keyed on its
	// own input ("gomod") finds it under "go".
	Lookup OwnedPackageNameLookup
	// Ecosystem is the reader's ecosystem column for the row; sbt rows keep
	// "sbt" because the reader never folds them onto maven.
	Ecosystem string
	// EntityName is the stored dependency name, in the case the manifest used.
	EntityName string
	// Version is the stored declared version.
	Version string
	// KeyHashLow24 is the low 24 bits of sha256(ecosystem|entity_name|version)
	// over the raw stored values, the reader's slot hash.
	KeyHashLow24 uint32
	// VirtualSlot is KeyHashLow24 modulo the pinned virtual slot count.
	VirtualSlot uint32
}

// OwnedPackageResolution is the result of one [FactStore.ResolveOwnedPackageNames]
// call.
type OwnedPackageResolution struct {
	// Targets are the matched owned targets, grouped by lookup ecosystem in
	// first-seen order and sorted inside each group by lookup name, ecosystem,
	// entity name and version, one per distinct identity.
	Targets []OwnedPackageResolvedTarget
	// Partial lists lookups whose match cannot be proven complete: a swift name
	// with no repository key (the feed spells a few packages as a bare name).
	// Such a name matches every repository with that final path segment, so it
	// can reach a fork, and it misses a package kept in a repository of another
	// name. The caller must treat them as "possibly more owned names exist".
	Partial []OwnedPackageNameLookup
}

// ResolveOwnedPackageNames maps OSV package names onto the owned dependency
// inventory: for each (ecosystem, name) it returns every active, non-tombstone
// stored (entity_name, version) whose name and version the vulnerability
// derivation would query for it, with the reader's key_hash_low24 and the
// virtual slot. A row the derivation skips (a non-exact version, an unexpanded
// variable, an sbt row with no consumption key) is not a target.
//
// Rows come from the owned-package reader's own active-generation statement
// text and package_manager bind set. Most ecosystems seek per folded name on
// the folded entity index (migration 247). The intersection runs on the folded
// name, but the hash is over the raw stored name: over-matching Go module paths
// that differ only by case costs a replan and is accepted; a hash over a folded
// string would be a silent miss and cannot occur here.
//
// Three cases have no index on what they compare, so each is a prefix read of
// one package manager. PyPI compares PEP 503 names on both sides, because a
// manifest keeps any run of separators. Swift compares
// [packageidentity.SwiftRepositoryKey] on both sides, and a bare feed name
// matches the final path segment of that key and is reported in Partial. The
// sbt "%%" read compares the binary artifact; a "%" row names its coordinate
// through the by-name read. See docs/internal/evidence/9777-owned-package-name-resolver.md
// for the cost of each.
//
// Caller contract: call once per pass with the whole hit list, never once per
// name. The PyPI and swift reads walk their package manager's prefix whatever
// the name count, so a one-name call costs about as much as a full one (about
// 53 ms for PyPI on the QA replica) and a per-name loop multiplies that walk.
func (s FactStore) ResolveOwnedPackageNames(
	ctx context.Context,
	lookups []OwnedPackageNameLookup,
) (OwnedPackageResolution, error) {
	if s.database == nil {
		return OwnedPackageResolution{}, fmt.Errorf("fact store database is required")
	}
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, contract.SpanOwnedPackageResolve)
	defer span.End()
	stats := &ownedResolverStats{}
	resolution, err := s.resolveOwnedPackageNames(ctx, lookups, stats)
	span.SetAttributes(
		attribute.Int("owned_package_resolver.lookups", len(lookups)),
		attribute.Int("owned_package_resolver.ecosystems", stats.ecosystems),
		attribute.Int("owned_package_resolver.folded_names", stats.foldedNames),
		attribute.Int("owned_package_resolver.statements", stats.statements),
		attribute.Int("owned_package_resolver.rows_read", stats.rowsRead),
		attribute.Int("owned_package_resolver.targets", len(resolution.Targets)),
		attribute.Int("owned_package_resolver.partial", len(resolution.Partial)),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "owned package name resolve failed")
		return OwnedPackageResolution{}, err
	}
	return resolution, nil
}

// ownedResolverStats counts the work one resolve call did, for its span.
type ownedResolverStats struct {
	ecosystems, foldedNames, statements, rowsRead int
}

func (s FactStore) resolveOwnedPackageNames(
	ctx context.Context,
	lookups []OwnedPackageNameLookup,
	stats *ownedResolverStats,
) (OwnedPackageResolution, error) {
	parts, err := ownedResolverPartsOnce()
	if err != nil {
		return OwnedPackageResolution{}, err
	}
	groups, order, err := groupOwnedLookups(lookups)
	if err != nil {
		return OwnedPackageResolution{}, err
	}
	stats.ecosystems = len(order)
	var resolution OwnedPackageResolution
	for _, ecosystem := range order {
		matched, partial, err := s.resolveOwnedEcosystem(ctx, parts, ecosystem, groups[ecosystem], stats)
		if err != nil {
			return OwnedPackageResolution{}, err
		}
		resolution.Targets = append(resolution.Targets, matched...)
		resolution.Partial = append(resolution.Partial, partial...)
	}
	return resolution, nil
}

// ownedLookupSpelling is one lookup with the folded names to seek for it and
// whether that list may not reach every owned row.
type ownedLookupSpelling struct {
	lookup  OwnedPackageNameLookup
	forms   []string
	partial bool
}

func groupOwnedLookups(lookups []OwnedPackageNameLookup) (map[string][]ownedLookupSpelling, []string, error) {
	owned := workflow.VulnerabilityOwnedDerivationEcosystems()
	groups := make(map[string][]ownedLookupSpelling)
	var order []string
	seen := make(map[OwnedPackageNameLookup]struct{}, len(lookups))
	for _, lookup := range lookups {
		ecosystem := workflow.CanonicalOwnedPackageEcosystem(lookup.Ecosystem)
		name := strings.Trim(lookup.Name, " ")
		if ecosystem == "" {
			return nil, nil, fmt.Errorf("resolve owned package names: a lookup has no ecosystem")
		}
		if !slices.Contains(owned, ecosystem) {
			return nil, nil, fmt.Errorf("resolve owned package names: ecosystem %q is outside the owned set %v", lookup.Ecosystem, owned)
		}
		if name == "" {
			continue
		}
		canonical := OwnedPackageNameLookup{Ecosystem: ecosystem, Name: name}
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		forms, partial := ownedNameLookupForms(ecosystem, name)
		if _, ok := groups[ecosystem]; !ok {
			order = append(order, ecosystem)
		}
		groups[ecosystem] = append(groups[ecosystem], ownedLookupSpelling{lookup: canonical, forms: forms, partial: partial})
	}
	return groups, order, nil
}

func (s FactStore) resolveOwnedEcosystem(
	ctx context.Context,
	parts ownedResolverParts,
	ecosystem string,
	spellings []ownedLookupSpelling,
	stats *ownedResolverStats,
) ([]OwnedPackageResolvedTarget, []OwnedPackageNameLookup, error) {
	managers := ownedPackageManagerFilterValues([]string{ecosystem}, true, true)
	byForm := make(map[string][]int)
	var partial []OwnedPackageNameLookup
	forms := make([]string, 0, len(spellings))
	for index, spelling := range spellings {
		if spelling.partial {
			partial = append(partial, spelling.lookup)
		}
		for _, form := range spelling.forms {
			if _, ok := byForm[form]; !ok {
				forms = append(forms, form)
			}
			byForm[form] = append(byForm[form], index)
		}
	}
	slices.Sort(forms)
	stats.foldedNames += len(forms)

	rows, err := s.readOwnedResolverRows(ctx, parts, ecosystem, managers, forms, stats)
	if err != nil {
		return nil, nil, err
	}
	return matchOwnedResolverRows(spellings, byForm, rows), partial, nil
}

// readOwnedResolverRows picks the read for an ecosystem: one prefix read for
// PyPI and swift, whose comparison no index serves, and chunked folded seeks
// for the rest. Maven adds the sbt binary-artifact read per chunk.
func (s FactStore) readOwnedResolverRows(
	ctx context.Context,
	parts ownedResolverParts,
	ecosystem string,
	managers, forms []string,
	stats *ownedResolverStats,
) ([]ownedResolverRow, error) {
	switch ecosystem {
	case "swift":
		return s.queryOwnedResolverRows(ctx, stats, ownedResolverSwiftQuery(parts), managers, nil)
	case "pypi":
		return s.queryOwnedResolverRows(ctx, stats, ownedResolverPyPIQuery(parts), managers, forms)
	}
	var rows []ownedResolverRow
	for chunk := range slices.Chunk(forms, ownedResolverNameChunk) {
		named, err := s.queryOwnedResolverRows(ctx, stats, ownedResolverByNameQuery(parts), managers, chunk)
		if err != nil {
			return nil, err
		}
		rows = append(rows, named...)
		if ecosystem == "maven" {
			binary, err := s.queryOwnedResolverRows(ctx, stats, ownedResolverBinaryArtifactQuery(parts), managers, chunk)
			if err != nil {
				return nil, err
			}
			rows = append(rows, binary...)
		}
	}
	return rows, nil
}

// ownedResolverRow is one reader-selected dependency identity.
type ownedResolverRow struct {
	ecosystem, name, version, sourceLocation string
	sbtCrossVersion, sbtBinaryArtifact       string
	keyHashLow24                             uint32
}

func (s FactStore) queryOwnedResolverRows(
	ctx context.Context,
	stats *ownedResolverStats,
	query string,
	managers []string,
	names []string,
) ([]ownedResolverRow, error) {
	var args []any
	if names == nil {
		// The swift statement binds only $1.
		args = []any{manifestDependencyQueryExecMode, managers}
	} else {
		args = []any{manifestDependencyQueryExecMode, managers, names}
	}
	stats.statements++
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve owned package names: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ownedResolverRow
	for rows.Next() {
		var (
			ecosystem, name, version, sourceLocation sql.NullString
			crossVersion, binaryArtifact             sql.NullString
			hash                                     sql.NullInt64
		)
		if err := rows.Scan(&ecosystem, &name, &version, &sourceLocation, &crossVersion, &binaryArtifact, &hash); err != nil {
			return nil, fmt.Errorf("resolve owned package names: %w", err)
		}
		row := ownedResolverRow{
			ecosystem:         strings.ToLower(strings.TrimSpace(ecosystem.String)),
			name:              strings.TrimSpace(name.String),
			version:           strings.TrimSpace(version.String),
			sourceLocation:    strings.TrimSpace(sourceLocation.String),
			sbtCrossVersion:   strings.TrimSpace(crossVersion.String),
			sbtBinaryArtifact: strings.TrimSpace(binaryArtifact.String),
			// #nosec G115 -- bounded: the low 24 bits of a sha256 are within
			// uint32; a NULL hash scans as 0, as the reader does.
			keyHashLow24: uint32(max(hash.Int64, 0)),
		}
		if row.ecosystem == "" || row.name == "" {
			continue
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve owned package names: %w", err)
	}
	stats.rowsRead += len(out)
	return out, nil
}

// ownedResolverDerivationEcosystem is the ecosystem the derivation queries a
// stored row under; sbt rows are queried as Maven.
func ownedResolverDerivationEcosystem(rowEcosystem string) string {
	if rowEcosystem == "sbt" {
		return "maven"
	}
	return rowEcosystem
}

// ownedResolverOSVName returns the package name the vulnerability derivation
// would query for a stored row, or "" when it queries none, including for a
// version the derivation refuses.
func ownedResolverOSVName(row ownedResolverRow) string {
	if _, ok := schedule.ExactOwnedDependencyVersionForEcosystem(ownedResolverDerivationEcosystem(row.ecosystem), row.version); !ok {
		return ""
	}
	if row.ecosystem == "swift" {
		return packageidentity.SwiftOSVSourceLocation(row.sourceLocation)
	}
	name := row.name
	if row.ecosystem == "sbt" {
		if len(packageidentity.ManifestConsumptionKeys(packageidentity.ManifestKeyInput{
			PackageManager:    row.ecosystem,
			Names:             []string{row.name},
			SBTCrossVersion:   row.sbtCrossVersion,
			SBTBinaryArtifact: row.sbtBinaryArtifact,
		})) == 0 {
			return ""
		}
		if row.sbtCrossVersion == "binary" {
			name = row.sbtBinaryArtifact
		}
	}
	if name == "" || strings.ContainsAny(name, "${}") {
		return ""
	}
	return name
}

// ownedResolverMatchKeys returns the keys a stored row answers to, the
// counterpart of [ownedNameLookupForms], or nil when the derivation queries
// nothing for the row. A swift row answers to its repository key and, for a
// bare feed name, to that key's final path segment. The key always holds a
// slash, so the two kinds of key cannot collide.
func ownedResolverMatchKeys(row ownedResolverRow) []string {
	osvName := ownedResolverOSVName(row)
	if osvName == "" {
		return nil
	}
	switch row.ecosystem {
	case "swift":
		key := packageidentity.SwiftRepositoryKey(osvName)
		return []string{key, key[strings.LastIndexByte(key, '/')+1:]}
	case "pypi":
		return []string{packageidentity.NormalizePyPIName(osvName)}
	default:
		return []string{packageidentity.LowerASCII(osvName)}
	}
}

func matchOwnedResolverRows(
	spellings []ownedLookupSpelling,
	byForm map[string][]int,
	rows []ownedResolverRow,
) []OwnedPackageResolvedTarget {
	type identity struct {
		lookup                   int
		ecosystem, name, version string
	}
	seen := make(map[identity]struct{})
	var out []OwnedPackageResolvedTarget
	for _, row := range rows {
		for _, matchKey := range ownedResolverMatchKeys(row) {
			for _, index := range byForm[matchKey] {
				key := identity{index, row.ecosystem, row.name, row.version}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, OwnedPackageResolvedTarget{
					Lookup:       spellings[index].lookup,
					Ecosystem:    row.ecosystem,
					EntityName:   row.name,
					Version:      row.version,
					KeyHashLow24: row.keyHashLow24,
					VirtualSlot:  row.keyHashLow24 % ownedResolverVirtualSlotCount,
				})
			}
		}
	}
	slices.SortFunc(out, func(a, b OwnedPackageResolvedTarget) int {
		return strings.Compare(
			a.Lookup.Name+"\x00"+a.Ecosystem+"\x00"+a.EntityName+"\x00"+a.Version,
			b.Lookup.Name+"\x00"+b.Ecosystem+"\x00"+b.EntityName+"\x00"+b.Version)
	})
	return out
}
