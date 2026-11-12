// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
	vulnerabilityv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerability/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychainfacts "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

const (
	// AdvisoryGroupFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:advisory_group coverage row names to resolve
	// through it (#10466).
	AdvisoryGroupFamilyOduName = "odu:ifa-advisory-group-family"

	// The two fixture repositories are ordinary git scopes: their
	// repository facts are what the projector turns into the Repository
	// nodes the AdvisoryGroup writer MATCHes (it never MERGEs one).
	advisoryGroupFamilyRepoAScopeID      = "git:repository:eshu-fixture-advisory-app"
	advisoryGroupFamilyRepoAGenerationID = "gen-ifa-advisory-group-family-app-1"
	advisoryGroupFamilyRepoAID           = "repository:eshu-fixture-advisory-app"
	advisoryGroupFamilyRepoALocalPath    = "/repo-advisory-app"
	advisoryGroupFamilyRepoASourceRunID  = "run-ifa-advisory-group-family-app-1"

	advisoryGroupFamilyRepoBScopeID      = "git:repository:eshu-fixture-advisory-lib"
	advisoryGroupFamilyRepoBGenerationID = "gen-ifa-advisory-group-family-lib-1"
	advisoryGroupFamilyRepoBID           = "repository:eshu-fixture-advisory-lib"
	advisoryGroupFamilyRepoBLocalPath    = "/repo-advisory-lib"
	advisoryGroupFamilyRepoBSourceRunID  = "run-ifa-advisory-group-family-lib-1"

	// The finding facts live in one vulnerability-intelligence scope, the
	// shape a committed supply_chain_impact pass leaves behind: its
	// writer upserts reducer_supply_chain_impact_finding facts under the
	// pass's own scope and generation, with the alias-group members under
	// payload.provenance.advisory_group_members (#10475). The scope also
	// carries one vulnerability.source_snapshot anchor fact so the live
	// drive creates this scope and generation before the findings are
	// admitted through the writer's fact-store port (see
	// advisoryGroupFamilyScopeAnchorFact for why it is not a
	// vulnerability.cve record).
	advisoryGroupFamilyFindingScopeID      = "osv:eshu-fixture-advisory-findings"
	advisoryGroupFamilyFindingGenerationID = "gen-ifa-advisory-group-family-findings-1"
	advisoryGroupFamilyFindingIntentID     = "intent-ifa-advisory-group-family-1"

	advisoryGroupFamilySchemaVersion = "1.0.0"
	advisoryGroupFamilyGitCollector  = "git"
	advisoryGroupFamilyOSVCollector  = "osv"

	// advisoryGroupFamilyGroupID is the one advisory group every finding
	// belongs to: CVE-keyed, so the winners reader matches it on cve_id.
	advisoryGroupFamilyGroupID = "CVE-2026-10466"
	// advisoryGroupFamilyAliasCVEID and advisoryGroupFamilyAliasGHSAID
	// are members no row carries as its own cve_id/advisory_id. They are
	// what makes the node-union assertion non-vacuous: BuildGroup folds
	// each row's own ids in even when Members is empty, so a fixture
	// whose members never leave the rows' own ids would prove nothing
	// about the provenance members read (#10466 arbiter condition).
	advisoryGroupFamilyAliasCVEID  = "CVE-2026-10467"
	advisoryGroupFamilyAdvisoryID  = "GHSA-10466-aaaa-bbbb"
	advisoryGroupFamilyAliasGHSAID = "GHSA-10466-cccc-dddd"
)

// advisoryGroupFamilyMembers is the upstream alias-group membership every
// finding carries under provenance, sorted the way the alias grouping
// emits it. The AdvisoryGroup node must end up carrying exactly this set,
// split into CVE and advisory classes.
var advisoryGroupFamilyMembers = []string{
	advisoryGroupFamilyGroupID,
	advisoryGroupFamilyAliasCVEID,
	advisoryGroupFamilyAdvisoryID,
	advisoryGroupFamilyAliasGHSAID,
}

// advisoryGroupFamilyFinding describes one
// reducer_supply_chain_impact_finding fact in the Odù.
type advisoryGroupFamilyFinding struct {
	Key             string
	RepositoryID    string
	PackageID       string
	PackageName     string
	PURL            string
	ObservedVersion string
	ImpactStatus    string
	Tombstone       bool
}

// advisoryGroupFamilyFindings holds three qualifying findings and three
// deliberate non-producers, all in the one group:
//
//   - app-left-1 and app-left-09: two winners for the same
//     (repository, package) at different observed versions. Each is its
//     own canonical winner (the canonical key carries purl and observed
//     version), and BuildGroup collapses them onto one edge, keeping
//     the lowest byte-order (ecosystem, purl) pair — @0.9.0.
//   - lib-right: the second repository's qualifying finding, the second
//     edge.
//   - orphan: no repository_id, so the winners reader drops it before
//     BuildGroup ever sees it.
//   - lib-fixed: not_affected_known_fixed, outside the reader's
//     qualifying status set.
//   - app-retracted: tombstoned, outside the winner select's active set.
//
// There is deliberately no projector trigger fact for
// advisory_group_projection here. No collector emits a followup marker
// for the domain — a committed supply_chain_impact pass enqueues it
// through AdvisoryGroupProjectionEnqueuer — so the live drive invokes
// the production writer admission, maintainer rebuild, and enqueuer at
// their ports between drains (eshu-ifa enqueue-advisory-groups)
// instead of inventing transport no production path emits (#10466
// arbiter ruling). Admission goes through the writer's fact-store
// port, not cassette ingestion, for a reason proven live during the
// lift: a finding payload carries reducer_domain=supply_chain_impact,
// so any re-projected generation holding finding facts makes the
// projector's generic per-fact builder enqueue a fresh impact pass
// that retracts them as superseded. Production findings never
// re-enter projection — the writer upserts them into an already
// projected generation — and the verb mirrors exactly that.
var advisoryGroupFamilyFindings = []advisoryGroupFamilyFinding{
	{Key: "app-left-1", RepositoryID: advisoryGroupFamilyRepoAID, PackageID: "pkg:npm/advisory-left", PackageName: "advisory-left", PURL: "pkg:npm/advisory-left@1.0.0", ObservedVersion: "1.0.0", ImpactStatus: "affected_exact"},
	{Key: "app-left-09", RepositoryID: advisoryGroupFamilyRepoAID, PackageID: "pkg:npm/advisory-left", PackageName: "advisory-left", PURL: "pkg:npm/advisory-left@0.9.0", ObservedVersion: "0.9.0", ImpactStatus: "affected_derived"},
	{Key: "lib-right", RepositoryID: advisoryGroupFamilyRepoBID, PackageID: "pkg:npm/advisory-right", PackageName: "advisory-right", PURL: "pkg:npm/advisory-right@2.0.0", ObservedVersion: "2.0.0", ImpactStatus: "affected_exact"},
	{Key: "orphan", RepositoryID: "", PackageID: "pkg:npm/advisory-orphan", PackageName: "advisory-orphan", PURL: "pkg:npm/advisory-orphan@3.0.0", ObservedVersion: "3.0.0", ImpactStatus: "affected_exact"},
	{Key: "lib-fixed", RepositoryID: advisoryGroupFamilyRepoBID, PackageID: "pkg:npm/advisory-fixed", PackageName: "advisory-fixed", PURL: "pkg:npm/advisory-fixed@4.0.0", ObservedVersion: "4.0.0", ImpactStatus: "not_affected_known_fixed"},
	{Key: "app-retracted", RepositoryID: advisoryGroupFamilyRepoAID, PackageID: "pkg:npm/advisory-retracted", PackageName: "advisory-retracted", PURL: "pkg:npm/advisory-retracted@5.0.0", ObservedVersion: "5.0.0", ImpactStatus: "affected_exact", Tombstone: true},
}

// advisoryGroupFamilyRepositoryFact builds one repository fact in the git
// collector's shape, mirroring the runtime_declaration fixture's: the
// projector requires source_run_id for the Repository phase row, and the
// local_path must be fixture-unique or the backend UNIQUE(path)
// constraint keeps exactly one colliding node.
func advisoryGroupFamilyRepositoryFact(scopeID, generationID, repoID, name, localPath, sourceRunID string) facts.Envelope {
	return facts.Envelope{
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factload.FactKindRepository,
		StableFactKey:    "repository:" + repoID,
		SchemaVersion:    advisoryGroupFamilySchemaVersion,
		CollectorKind:    advisoryGroupFamilyGitCollector,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload: map[string]any{
			"graph_id":      repoID,
			"graph_kind":    "repository",
			"repo_id":       repoID,
			"name":          name,
			"local_path":    localPath,
			"source_run_id": sourceRunID,
		},
	}
}

// advisoryGroupFamilyFindingPayload encodes one finding through
// factschema.EncodeReducerSupplyChainImpactFinding — the production
// writer's own encoder — in the writer's payload shape: members under
// provenance, never a hand-built map and never a top-level members key
// no production writer emits (#10475).
func advisoryGroupFamilyFindingPayload(finding advisoryGroupFamilyFinding) map[string]any {
	payload, err := factschema.EncodeReducerSupplyChainImpactFinding(reducerderivedv1.SupplyChainImpactFinding{
		ReducerDomain:   "supply_chain_impact",
		IntentID:        advisoryGroupFamilyFindingIntentID,
		ScopeID:         advisoryGroupFamilyFindingScopeID,
		GenerationID:    advisoryGroupFamilyFindingGenerationID,
		SourceSystem:    advisoryGroupFamilyOSVCollector,
		Cause:           "vulnerability evidence observed",
		FindingID:       "finding-ifa-advisory-group-" + finding.Key,
		CVEID:           advisoryGroupFamilyGroupID,
		AdvisoryID:      advisoryGroupFamilyAdvisoryID,
		PackageID:       finding.PackageID,
		Ecosystem:       "npm",
		PackageName:     finding.PackageName,
		PURL:            finding.PURL,
		ObservedVersion: finding.ObservedVersion,
		SubjectDigest:   advisoryGroupFamilySubjectDigest(finding.PURL),
		MatchReason:     "exact_version_match",
		ImpactStatus:    finding.ImpactStatus,
		Confidence:      "high",
		RepositoryID:    finding.RepositoryID,
		WorkloadIDs:     []string{},
		DeploymentIDs:   []string{},
		ServiceIDs:      []string{},
		Environments:    []string{},
		// PriorityReasonCodes, PriorityContributions, CatalogEntityRefs,
		// CatalogOwnerRefs, MissingEvidence, EvidencePath,
		// EvidenceFactIDs, and SourceLayers are non-nil in every payload
		// the production writer emits (its nonNilStrings/nonNilMapSlice
		// normalization), so the fixture carries them the same way.
		PriorityReasonCodes:   []string{},
		PriorityContributions: []map[string]any{},
		CatalogEntityRefs:     []string{},
		CatalogOwnerRefs:      []string{},
		MissingEvidence:       []string{},
		EvidencePath:          []string{},
		EvidenceFactIDs:       []string{},
		SourceLayers:          []string{"source_declaration", "observed_resource"},
		Provenance: map[string]any{
			"advisory_group_members": append([]string(nil), advisoryGroupFamilyMembers...),
		},
	})
	if err != nil {
		panic(fmt.Sprintf("familyodu: catalog_seed %s: encode finding payload %q: %v",
			AdvisoryGroupFamilyOduName, finding.Key, err))
	}
	return payload
}

// advisoryGroupFamilySubjectDigest derives the finding's subject
// digest deterministically from its package URL. The schema requires
// the field and the cassette digest guard requires it well-formed; a
// fixture package has no real artifact digest, so the sha256 of its
// purl is the honest synthetic stand-in — stable across regenerations
// and distinct per subject.
func advisoryGroupFamilySubjectDigest(purl string) string {
	sum := sha256.Sum256([]byte(purl))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// advisoryGroupFamilyScopeAnchorFact builds the findings scope's one
// vulnerability.source_snapshot record through the production OSV
// encoder: the collection-boundary fact a real OSV generation carries.
// It anchors the scope generation for the live drive. It is
// deliberately NOT a vulnerability.cve record: that kind is a
// supply_chain_impact pass candidate, and a pass over this scope —
// whenever cross-scope readiness let it claim — would retract the
// admitted findings as superseded and delete the projected group
// (proven live on this branch). The snapshot kind triggers no pass,
// and holder fan-out is package-keyed to changed manifest packages,
// which this fixture's packages never are in another family's view.
func advisoryGroupFamilyScopeAnchorFact() facts.Envelope {
	ecosystem := "npm"
	payload, err := factschema.EncodeVulnerabilitySourceSnapshot(vulnerabilityv1.SourceSnapshot{
		Source:    advisoryGroupFamilyOSVCollector,
		Complete:  true,
		Ecosystem: &ecosystem,
	})
	if err != nil {
		panic(fmt.Sprintf("familyodu: catalog_seed %s: encode vulnerability.source_snapshot payload: %v",
			AdvisoryGroupFamilyOduName, err))
	}
	return facts.Envelope{
		ScopeID:          advisoryGroupFamilyFindingScopeID,
		GenerationID:     advisoryGroupFamilyFindingGenerationID,
		FactKind:         supplychainfacts.VulnerabilitySourceSnapshotFactKind,
		StableFactKey:    "vulnerability_source_snapshot:ifa-advisory-group:npm",
		SchemaVersion:    advisoryGroupFamilySchemaVersion,
		CollectorKind:    advisoryGroupFamilyOSVCollector,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload:          payload,
	}
}

// AdvisoryGroupFamilyOdu builds the cataloged Odù for the advisory_group
// direct-materialization family: two repository facts (one per git
// scope), one vulnerability.source_snapshot anchor fact, and six
// reducer_supply_chain_impact_finding facts in one
// vulnerability-intelligence scope, encoded through the production
// writer's factschema encoder. It panics on an encode failure: that
// means the payload contract moved under a committed fixture, and every
// coverage claim built on it is already void.
func AdvisoryGroupFamilyOdu() CatalogOdu {
	out := make([]facts.Envelope, 0, len(advisoryGroupFamilyFindings)+3)
	out = append(out,
		advisoryGroupFamilyRepositoryFact(
			advisoryGroupFamilyRepoAScopeID, advisoryGroupFamilyRepoAGenerationID,
			advisoryGroupFamilyRepoAID, "advisory-app",
			advisoryGroupFamilyRepoALocalPath, advisoryGroupFamilyRepoASourceRunID),
		advisoryGroupFamilyRepositoryFact(
			advisoryGroupFamilyRepoBScopeID, advisoryGroupFamilyRepoBGenerationID,
			advisoryGroupFamilyRepoBID, "advisory-lib",
			advisoryGroupFamilyRepoBLocalPath, advisoryGroupFamilyRepoBSourceRunID),
		advisoryGroupFamilyScopeAnchorFact(),
	)
	for _, finding := range advisoryGroupFamilyFindings {
		out = append(out, facts.Envelope{
			ScopeID:          advisoryGroupFamilyFindingScopeID,
			GenerationID:     advisoryGroupFamilyFindingGenerationID,
			FactKind:         facts.ReducerSupplyChainImpactFindingFactKind,
			StableFactKey:    "supply_chain_impact_finding:ifa-advisory-group:" + finding.Key,
			SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
			CollectorKind:    advisoryGroupFamilyOSVCollector,
			SourceConfidence: facts.SourceConfidenceInferred,
			IsTombstone:      finding.Tombstone,
			Payload:          advisoryGroupFamilyFindingPayload(finding),
		})
	}
	return CatalogOdu{
		Odu: Odu{Name: AdvisoryGroupFamilyOduName, Facts: out},
		Detail: "nine facts for the direct-materialization advisory_group family: two repositories, one vulnerability.source_snapshot anchor record, and six supply-chain impact findings in one CVE-keyed alias group — " +
			"two conflicting winners for the app repository collapse onto one AFFECTED_BY edge and the lib repository contributes the second, " +
			"while the repository-less, known-fixed, and tombstoned findings produce nothing; every finding carries the four-member alias union under provenance",
	}
}
