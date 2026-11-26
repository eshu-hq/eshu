// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"fmt"
	"regexp"
)

// NornicDBBackendVersionCypher reports the connected backend's self-reported
// version through the same component procedure the IFA manifest shim uses
// over HTTP (scripts/lib/ifa_scheduled_manifest.py). The boot log prints the
// same string as "NornicDB v<version>" (observed: "Starting NornicDB
// v1.5.2-hotfix"); dbms.components() answers it as versions[0] under the
// "NornicDB" component name. No name filter: NornicDB reports a single
// component row, and an empty answer fails closed in
// NornicDBComponentVersionFromCell. Schema executors share this literal so
// the probe query cannot drift between call sites.
const NornicDBBackendVersionCypher = "CALL dbms.components() YIELD name, versions RETURN versions"

// BackendVersionQuerier is implemented by schema executors that can report
// the connected backend's self-reported version string. The schema gate
// consumes it; drivers implement it with nornicDBBackendVersionCypher, test
// fakes with a configured string. A missing implementation is a refusal, not
// a skip: an unproven backend never receives the 1.5.x-gated DDL.
type BackendVersionQuerier interface {
	QueryBackendVersion(context.Context) (string, error)
}

// nornicDBMinimumSupportedMajor and nornicDBMinimumSupportedMinor floor the
// backends that may receive the 1.5.x-gated statement list (#11535): the uid
// uniqueness constraint creates its serving backing index only on 1.5.x, so
// on anything older the DROP guards for the retired lookup indexes would
// leave uid reads unindexed.
const (
	nornicDBMinimumSupportedMajor = 1
	nornicDBMinimumSupportedMinor = 5
)

var nornicDBVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)`)

// parseNornicDBBackendVersion extracts the first major.minor pair from a
// self-reported backend version string ("NornicDB v1.5.2-hotfix" -> 1, 5).
// It reports false when the string carries no numeric pair, so callers fail
// closed instead of guessing.
func parseNornicDBBackendVersion(reported string) (major, minor int, ok bool) {
	match := nornicDBVersionPattern.FindStringSubmatch(reported)
	if match == nil {
		return 0, 0, false
	}
	var maj, min int
	if _, err := fmt.Sscanf(match[1], "%d", &maj); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(match[2], "%d", &min); err != nil {
		return 0, 0, false
	}
	return maj, min, true
}

// NornicDBComponentVersionFromCell decodes the versions[0] element of a
// dbms.components() row into the reported version string. Drivers hand over
// their native cell value; anything that is not a non-empty string fails
// closed.
func NornicDBComponentVersionFromCell(cell any) (string, error) {
	versions, ok := cell.([]any)
	if !ok || len(versions) == 0 {
		return "", fmt.Errorf("nornicdb backend version: dbms.components() returned no versions")
	}
	reported, ok := versions[0].(string)
	if !ok || reported == "" {
		return "", fmt.Errorf("nornicdb backend version: dbms.components() versions[0] is not a version string")
	}
	return reported, nil
}

// requireNornicDBBackendVersion refuses the 1.5.x-gated statement list unless
// the executor proves the connected backend is NornicDB 1.5 or newer. It
// fails closed on every other outcome: an executor that cannot report a
// version, a query error, an unparseable report, or a version below the
// floor. Neo4j never reaches this gate (its dialect does not set
// dropUIDLookupIndexes), so its path is untouched.
func requireNornicDBBackendVersion(ctx context.Context, executor CypherExecutor) error {
	querier, ok := executor.(BackendVersionQuerier)
	if !ok {
		return fmt.Errorf("nornicdb schema apply refused: executor %T cannot report the backend version, "+
			"and the #11535 statement list must only ever execute against NornicDB >= %d.%d",
			executor, nornicDBMinimumSupportedMajor, nornicDBMinimumSupportedMinor)
	}
	reported, err := querier.QueryBackendVersion(ctx)
	if err != nil {
		return fmt.Errorf("nornicdb schema apply refused: backend version query failed: %w", err)
	}
	major, minor, ok := parseNornicDBBackendVersion(reported)
	if !ok {
		return fmt.Errorf("nornicdb schema apply refused: unparseable backend version %q, "+
			"and the #11535 statement list must only ever execute against NornicDB >= %d.%d",
			reported, nornicDBMinimumSupportedMajor, nornicDBMinimumSupportedMinor)
	}
	if major > nornicDBMinimumSupportedMajor ||
		(major == nornicDBMinimumSupportedMajor && minor >= nornicDBMinimumSupportedMinor) {
		return nil
	}
	return fmt.Errorf("nornicdb schema apply refused: backend version %q is below the #11535 floor %d.%d "+
		"(the uid uniqueness constraint creates its serving backing index only on 1.5.x)",
		reported, nornicDBMinimumSupportedMajor, nornicDBMinimumSupportedMinor)
}
