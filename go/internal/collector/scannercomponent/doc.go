// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scannercomponent is the neutral leaf that owns the scanner
// vocabulary and the component-admission contract after the #8878 in-tree
// scanner removal.
//
// It receives the frozen analyzer, target, and failure vocabularies plus the
// admission half of the former in-tree scanner component package:
// ManifestFor, AnalyzerFor, VerifySeededArtifact, VerifySatelliteSeededArtifact,
// SatelliteSeedPins, RequiredGrants, the
// component IDs (including `SatelliteDocumentImportID` and
// `SatelliteMediaImportID`), the manifest Version, and the artifact digest pins. The execution half (claim
// input, analyzers, runners, paging, seeding) is not in this tree: its
// sources were removed from eshu in #9741, and the pinned images are built
// and attested by eshu-hq/eshu-scanner-worker (release run 41401606694, main
// b18339cc43). #8878 box 10 stays open until that re-pin is deployed and
// accepted. This package never executes an analyzer.
//
// It also owns the Go reachability value contract: the go-reachability://
// scope id, the target stored in a run's requested_scope_set, and the rotation
// page types. These are plain values with no store handle, so the coordinator,
// the Postgres reader, and the extension host agree without importing each other.
// RequiredGrantsFor names an analyzer's grants before its image exists; ManifestFor
// and VerifySeededArtifact still refuse a component with no published digest pin.
//
// Import discipline: the coordinator planner and the component admission
// callers both import this leaf; they never import each other. No new SDK
// vocabulary is published from here — an SDK vocab publication is a separate
// versioned contract change.
//
// Every frozen string is asserted verbatim by vocabulary_test.go. A rename,
// removal, or value change breaks that test on purpose: the satellite, the
// planner, telemetry dimensions, and status classes all key on these exact
// strings.
package scannercomponent
