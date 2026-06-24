// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// RDSInstancePostureFactKind identifies one derived security/operations
	// posture observation for an RDS DB instance or Aurora DB cluster. It is
	// metadata-only control-plane evidence: derived booleans, retention
	// windows, and KMS/parameter/option-group identifiers reported by the RDS
	// describe APIs. It never carries database contents, master usernames,
	// connection secrets, snapshot payloads, log bodies, or Performance
	// Insights samples. The fact is source evidence only; reducers own any
	// graph edges, internet-exposure derivation, or posture truth promotion.
	RDSInstancePostureFactKind = "rds_instance_posture"

	// RDSPostureSchemaVersionV1 is the first RDS posture fact schema.
	RDSPostureSchemaVersionV1 = "1.0.0"
)

var rdsPostureFactKinds = []string{
	RDSInstancePostureFactKind,
}

var rdsPostureSchemaVersions = map[string]string{
	RDSInstancePostureFactKind: RDSPostureSchemaVersionV1,
}

// RDSPostureFactKinds returns the accepted RDS posture fact kinds in their
// source-contract order. The returned slice is a copy; callers may mutate it
// without affecting the registry.
func RDSPostureFactKinds() []string {
	return slices.Clone(rdsPostureFactKinds)
}

// RDSPostureSchemaVersion returns the schema version for an RDS posture fact
// kind and reports whether the kind is registered.
func RDSPostureSchemaVersion(factKind string) (string, bool) {
	version, ok := rdsPostureSchemaVersions[factKind]
	return version, ok
}
