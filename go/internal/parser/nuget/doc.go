// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package nuget parses MSBuild project files (.csproj) and the two shared
// MSBuild props files Directory.Packages.props and Directory.Build.props into
// the parent parser engine's content_entity dependency rows so the supply-chain reducer can match
// NuGet package-registry identity against repository dependency truth.
//
// Parse decodes the project XML with encoding/xml and emits one "variables" row
// per <PackageReference> found in any <ItemGroup>, whether the package name
// comes from Include or Update. Each row records the version text exactly as
// declared (requested_version), the version this parser could prove (value),
// the PrivateAssets/IncludeAssets/ExcludeAssets asset lists, and the item-level
// and group-level Condition attributes both separately (condition_item,
// condition_group) and pre-merged with the item-level attribute winning
// (condition), so a multi-targeted project's repeated PackageReference rows stay
// distinguishable downstream.
//
// MSBuild $(Property) references in a version are substituted only from
// <PropertyGroup> elements in the same file. A property declared twice with
// different values, or not declared at all, leaves the raw text in value and
// stamps the row version_evidence "ambiguous_msbuild_property" or
// "unresolved_msbuild_property" plus partial_evidence rather than inventing a
// version. dependency_scope is "test" when the package name is one of five
// known .NET test packages or simply contains "test", "development" when
// PrivateAssets lists "all" or IncludeAssets lists "none", and "runtime"
// otherwise.
//
// The package never invokes MSBuild, evaluates Condition expressions, restores
// packages, or reads a sibling file: a property that only a sibling file defines
// stays unresolved, and a .csproj is never completed from a props file.
//
// Directory.Packages.props and Directory.Build.props are scanned on their own,
// fail-closed, with a streaming XML tokenizer (see props.go and the README
// grammar). PackageVersion, GlobalPackageReference, and PackageReference items
// become dependency rows in their own section; literal PropertyGroup entries
// become msbuild_property rows. A construct outside the grammar yields no row
// and a counter in the msbuild_props_limits bucket. A later Update or Remove,
// a duplicate Include, a conditioned metadata child, an Exclude, an Import or
// other unread Project child, or an unproven ManagePackageVersionsCentrally
// keeps the row but marks it partial with an item_ambiguity reason
// (props_items.go). Input size, nesting depth, row counts, tracked item names,
// and field lengths are bounded (Max* constants in props.go). A PackageVersion
// row is a version pin, not consumption: the reducer, the Postgres consumption
// sidecar, and the readiness package.consumption count all exclude rows flagged
// central_version_declaration. MSBuild %XX escapes are not unescaped, so an
// escaped version is unresolved and an escaped item spec counts as a wildcard.
// A GlobalPackageReference is always development scope, because NuGet forces
// PrivateAssets="All" on it; other rows omit dependency_scope when the asset
// metadata that decides it is conditioned or unresolved.
//
// NuGet lockfiles (packages.lock.json) are JSON and are parsed by
// go/internal/parser/json instead; they share the same row contract so the
// reducer treats NuGet manifest and lockfile evidence consistently.
package nuget
