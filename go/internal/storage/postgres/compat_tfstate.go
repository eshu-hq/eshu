// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tfstate"
)

// This file is the postgres root's compatibility surface for the tfstate
// family, which moved to [tfstate] (issue #8361). It is the first compat
// file in this package: no compat_*.go existed before the tfstate move, so
// a later family move adds a stanza to this file and never creates a new
// compat_*.go. Each entry is deleted once its last caller has moved; see
// the importer-migration child issues.
//
// The aliases below keep every existing caller compiling unchanged: the
// root's own AWS runtime-drift consumer
// (aws_cloud_runtime_drift_evidence.go), the eight cmd/ wiring sites, and
// the one tfstate-named test that stays in root
// (tfstate_no_plaintext_persistence_test.go, an ingestion-store test that
// references no tfstate symbol). Everything else the family exports is
// reached as tfstate.X.

// PostgresTerraformBackendQuery lists canonical backend rows. See
// [tfstate.PostgresTerraformBackendQuery].
type PostgresTerraformBackendQuery = tfstate.PostgresTerraformBackendQuery

// TerraformStateBackendFactReader reads backend facts. See
// [tfstate.TerraformStateBackendFactReader].
type TerraformStateBackendFactReader = tfstate.TerraformStateBackendFactReader

// TerraformStateGitReadinessChecker checks git generation readiness. See
// [tfstate.TerraformStateGitReadinessChecker].
type TerraformStateGitReadinessChecker = tfstate.TerraformStateGitReadinessChecker

// TerraformStatePriorSnapshotReader reads prior snapshot metadata. See
// [tfstate.TerraformStatePriorSnapshotReader].
type TerraformStatePriorSnapshotReader = tfstate.TerraformStatePriorSnapshotReader

// PostgresDriftEvidenceLoader loads drift evidence. See
// [tfstate.PostgresDriftEvidenceLoader].
type PostgresDriftEvidenceLoader = tfstate.PostgresDriftEvidenceLoader

// coerceJSONString formats one leaf JSON value into the canonical drift
// string. It lives in [tfstate] (issue #8361) but stays callable under its
// root name because the aws, cloud, and identity families use it directly
// (about fourteen files); moving every caller is follow-up work. See
// [tfstate.CoerceJSONString].
func coerceJSONString(value any) string {
	return tfstate.CoerceJSONString(value)
}
