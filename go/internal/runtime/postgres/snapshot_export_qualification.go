// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// snapshotExportQualifiedVersionNum is the first PostgreSQL 18 release that
// qualifies for shared-snapshot export without an operator declaration
// (#7957). REL_18_STABLE carries both upstream fixes the shared-snapshot path
// depends on: 74c9832ca610 (pg_export_snapshot during recovery must include
// subxip[], backpatch of ef89c839cdc8) and 3977cde272bc (snapshot import must
// not let the importer's xmin race VACUUM, backpatch of ca99016f939d). The
// first release tagged after both is 18.7. A patched 18.6 rebuild still reports
// 180006, so it qualifies only through ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS.
const snapshotExportQualifiedVersionNum = 180007

// snapshotExportQualifiedMajor bounds the floor to PostgreSQL 18. Any other
// major version is unqualified unless an operator declares its exact build.
const snapshotExportQualifiedMajor = 18

const (
	snapshotExportBuildsEnv           = "ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS"
	snapshotExportEvidenceMaxRunes    = 256
	snapshotExportReasonVersionFloor  = "version_floor"
	snapshotExportReasonDeclaredBuild = "declared_build"
	// snapshotExportReasonNoDeclaration and snapshotExportReasonUndeclared
	// are the only two denial reasons, so the denial counter's reason label is
	// a closed set: no declaration was configured, or none matched the build.
	snapshotExportReasonNoDeclaration = "no_declaration"
	snapshotExportReasonUndeclared    = "undeclared_build"
)

// bareServerVersionLabel matches a server_version that carries no
// --with-extra-version suffix. Such a declaration matches every build with
// that label, so it cannot detect an image-pin regression.
var bareServerVersionLabel = regexp.MustCompile(`^\d+(\.\d+)?$`)

// SnapshotExportBuild declares one PostgreSQL reader build an operator has
// qualified for shared-snapshot export. ServerVersion must equal
// current_setting('server_version') exactly; Evidence names what proves the
// build carries the fixes (an image digest, a package revision, or a recipe
// reference). Neither field holds a credential.
type SnapshotExportBuild struct {
	ServerVersion string `json:"server_version"`
	Evidence      string `json:"evidence"`
}

// parseSnapshotExportBuilds reads the operator declaration. A blank value is
// unset, which leaves the reader deny-by-default below the version floor.
func parseSnapshotExportBuilds(raw string) ([]SnapshotExportBuild, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var builds []SnapshotExportBuild
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&builds); err != nil || len(builds) == 0 {
		return nil, fmt.Errorf("%s must be a nonempty JSON array of {server_version, evidence} objects", snapshotExportBuildsEnv)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s contains trailing data", snapshotExportBuildsEnv)
	}
	if err := validateSnapshotExportBuilds(builds); err != nil {
		return nil, err
	}
	return builds, nil
}

// validateSnapshotExportBuilds enforces the declaration rules for a parsed or
// directly constructed Config.
func validateSnapshotExportBuilds(builds []SnapshotExportBuild) error {
	seen := make(map[string]bool, len(builds))
	for _, build := range builds {
		if build.ServerVersion == "" {
			return fmt.Errorf("%s entries need a nonempty server_version", snapshotExportBuildsEnv)
		}
		if build.Evidence == "" || utf8.RuneCountInString(build.Evidence) > snapshotExportEvidenceMaxRunes {
			return fmt.Errorf("%s entries need nonempty evidence of at most %d characters", snapshotExportBuildsEnv, snapshotExportEvidenceMaxRunes)
		}
		if seen[build.ServerVersion] {
			return fmt.Errorf("%s contains a duplicate server_version", snapshotExportBuildsEnv)
		}
		seen[build.ServerVersion] = true
	}
	return nil
}

// snapshotExportVerdict is one immutable qualification decision for a reader
// pool or fleet member, with the build evidence an operator log needs.
type snapshotExportVerdict struct {
	qualified        bool
	reason           string
	evidence         string
	serverVersion    string
	serverVersionNum int
	inRecovery       bool
}

// evaluateSnapshotExport decides whether one reader build may export a shared
// snapshot. A declaration matches only on exact string equality with
// current_setting('server_version'): no trimming, case folding, or prefix
// matching. Without a match, only PostgreSQL 18 at or above the floor
// qualifies.
func evaluateSnapshotExport(serverVersion string, serverVersionNum int, builds []SnapshotExportBuild) (qualified bool, reason, evidence string) {
	for _, build := range builds {
		if build.ServerVersion == serverVersion {
			return true, snapshotExportReasonDeclaredBuild, build.Evidence
		}
	}
	if serverVersionNum/10000 == snapshotExportQualifiedMajor && serverVersionNum >= snapshotExportQualifiedVersionNum {
		return true, snapshotExportReasonVersionFloor, ""
	}
	return false, snapshotExportDenialReason(builds), ""
}

// snapshotExportDenialReason is the closed denial label. It depends only on
// whether any declaration is configured, so it is known even before a reader
// has reported its build.
func snapshotExportDenialReason(builds []SnapshotExportBuild) string {
	if len(builds) == 0 {
		return snapshotExportReasonNoDeclaration
	}
	return snapshotExportReasonUndeclared
}

// snapshotExportQualification holds the current verdict for one reader pool or
// fleet member. Borrow paths refresh it from the identity statement they
// already run, and the snapshot-set fast path and pool-metrics scrape read it
// with one atomic load. A nil holder or a holder with no observation yet is
// unqualified: no build was proven.
type snapshotExportQualification struct {
	builds  []SnapshotExportBuild
	verdict atomic.Pointer[snapshotExportVerdict]
}

func newSnapshotExportQualification(builds []SnapshotExportBuild) *snapshotExportQualification {
	return &snapshotExportQualification{builds: builds}
}

// observe recomputes the verdict for one build report and publishes it only
// when it changed, so an unchanged build costs no allocation. Concurrent
// observers of different builds publish last-writer-wins; every borrow also
// carries its own per-connection verdict, so a snapshot set never relies on a
// verdict another connection reported.
func (q *snapshotExportQualification) observe(serverVersion string, serverVersionNum int, inRecovery bool) bool {
	var builds []SnapshotExportBuild
	if q != nil {
		builds = q.builds
	}
	qualified, reason, evidence := evaluateSnapshotExport(serverVersion, serverVersionNum, builds)
	if q == nil {
		return qualified
	}
	current := q.verdict.Load()
	if current != nil && current.qualified == qualified && current.reason == reason && current.evidence == evidence &&
		current.serverVersion == serverVersion && current.serverVersionNum == serverVersionNum && current.inRecovery == inRecovery {
		return qualified
	}
	q.verdict.Store(&snapshotExportVerdict{
		qualified: qualified, reason: reason, evidence: evidence,
		serverVersion: serverVersion, serverVersionNum: serverVersionNum, inRecovery: inRecovery,
	})
	return qualified
}

// current returns the last published verdict, or nil when none was observed.
func (q *snapshotExportQualification) current() *snapshotExportVerdict {
	if q == nil {
		return nil
	}
	return q.verdict.Load()
}

// qualified reports whether the last observed build may export a snapshot.
func (q *snapshotExportQualification) qualified() bool {
	verdict := q.current()
	return verdict != nil && verdict.qualified
}

// denialReason is the closed counter label for a refused request.
func (q *snapshotExportQualification) denialReason() string {
	if q == nil {
		return snapshotExportReasonNoDeclaration
	}
	return snapshotExportDenialReason(q.builds)
}
