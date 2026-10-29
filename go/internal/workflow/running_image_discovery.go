// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

// RunningImageDiscoveryCursor is a durable position in one bounded source
// cycle. Version is a compare-and-swap fence for concurrent coordinators.
// Observation IDs bound the finite cycle; they do not claim commit ordering.
type RunningImageDiscoveryCursor struct {
	Version                  int64  `json:"version"`
	CycleUpperObservationID  int64  `json:"cycle_upper_observation_id"`
	AfterObservationID       int64  `json:"after_observation_id"`
	AfterAuthorizationDomain string `json:"after_authorization_domain"`
}

// RunningImageDiscoveryRequest selects source observations under the server's
// explicit local allowlist or current hosted tenant-scope grants.
type RunningImageDiscoveryRequest struct {
	ScannerCollectorInstanceID string                        `json:"scanner_collector_instance_id"`
	AuthorizationNamespace     ScannerAuthorizationNamespace `json:"authorization_namespace"`
	TenantID                   string                        `json:"tenant_id"`
	WorkspaceID                string                        `json:"workspace_id"`
	SubjectClass               string                        `json:"subject_class"`
	PolicyRevisionHash         string                        `json:"policy_revision_hash"`
	DiscoveryPolicyRevision    int64                         `json:"discovery_policy_revision"`
	ExpectedRolePolicyRevision int64                         `json:"expected_role_policy_revision"`
	ExpectedDatabaseIdentity   string                        `json:"expected_database_identity"`
	LocalSourceScopeIDs        []string                      `json:"local_source_scope_ids"`
	Limit                      int                           `json:"limit"`
}

// RunningImageObservationCandidate contains an immutable, currently eligible
// source and a manifest resolved by protected evidence. Admission rechecks all
// authority; this read result alone never authorizes a work item.
type RunningImageObservationCandidate struct {
	ObservationID          int64
	ProducerInstanceID     string
	SourceScopeID          string
	SourceGenerationID     string
	ObjectID               string
	SlotID                 string
	ProducerClass          RunningImageProducerClass
	ObservationClass       RunningImageObservationClass
	ObservedRevision       string
	Registry               string
	Repository             string
	RuntimeImageReference  string
	RuntimeDigest          string
	RuntimeDigestKind      RunningImageDigestKind
	ManifestDigest         string
	ManifestVerificationID string
	AuthorizationDomain    string
}

// RunningImageDiscoveryPage returns only candidates whose current source,
// boundary and registry witnesses passed before the SQL LIMIT. An exhausted
// page wraps on the next durable cursor advance, including an empty page.
type RunningImageDiscoveryPage struct {
	Candidates     []RunningImageObservationCandidate
	ExpectedCursor RunningImageDiscoveryCursor
	NextCursor     RunningImageDiscoveryCursor
	Exhausted      bool
	// Bounded skip counts are server codes and never include private locators.
	SkippedTagOnly          int
	SkippedUnresolvedConfig int
}
