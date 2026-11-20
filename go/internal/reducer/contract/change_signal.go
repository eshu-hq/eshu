// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contract change signals for #10878.
//
// Each of the five incremental domains names one change signal that decides
// whether a run re-derives truth or skips: an input fingerprint (recorded
// after verified writes, compared on reopen, exactly the #8283
// deployable_unit_correlation contract), a marked key (a dirty set captured
// at fact-save time that narrows the re-derivation scope), or a delta
// snapshot (the added/changed/removed split of a generation diff, where
// removals always retract and never widen to whole-repo deletes per the
// delta-generation dead-letter rule).
//
// The helpers here are pure-Go set operations over values the stores
// compute in SQL: the AWS loader is already ARN-keyed
// (aws_cloud_runtime_drift_evidence.go), the identity epoch fingerprint is
// the ordered-set SHA-256 the probe query returns
// (facts_active_container_image_identity.go), the supply-chain stamp is the
// one-hop CVE set scoped_pass.go already keys on, and the search-document
// fingerprint mirrors the epoch's ordered-set digest shape. Handlers and
// reopen listings consume these signals; this file defines their shape and
// comparison, not their storage.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
)

// Logic versions for the #10878 change-signal fingerprints. Each follows the
// DeployableUnitCorrelationLogicVersion contract in reopen_fingerprint.go: a
// recorded fingerprint re-opens when its version differs, so any change that
// can produce different graph truth from the same facts MUST bump the owning
// domain's version, and a change that cannot MUST NOT.
const (
	// AWSRuntimeDriftLogicVersion versions the AWS runtime-drift change
	// signal: the ARN-set fingerprint plus the per-ARN payload identity,
	// the matched active tfstate generation per ARN, resolved config
	// anchor commits, and this version.
	AWSRuntimeDriftLogicVersion = 1
	// SupplyChainImpactLogicVersion versions the supply-chain marked key:
	// the dirty (CVE, package, PURL) set deriving the scoped stamp.
	SupplyChainImpactLogicVersion = 1
	// WorkloadMaterializationLogicVersion versions the workload
	// materialization input fingerprint: the work unit's own generation
	// plus ResolvedInputFingerprintSQL over the resolved rows it reads.
	WorkloadMaterializationLogicVersion = 1
	// ContainerImageIdentityLogicVersion versions the container-identity
	// epoch gate: count, max observed_at, and the ordered-set SHA-256
	// fingerprint of active (scope, generation) pairs.
	ContainerImageIdentityLogicVersion = 1
	// SearchDocumentsLogicVersion versions the search-documents signal:
	// the per-repository document fingerprint plus dirty document keys.
	SearchDocumentsLogicVersion = 1
)

// ARNRecordDiff is the delta snapshot of one AWS generation diff: ARNs
// keyed by their payload identity (content hash of the AWS fact plus the
// matched tfstate/config identity for that ARN). Added and Changed ARNs are
// (re)classified; Removed ARNs retract their findings. A delta generation
// carrying this diff carries changed files only, so a narrowed retract must
// never widen to a whole-repo delete.
type ARNRecordDiff struct {
	Added   []string
	Changed []string
	Removed []string
}

// HasChanges reports whether the diff carries any work.
func (d ARNRecordDiff) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Changed) > 0 || len(d.Removed) > 0
}

// ChangedKeys returns the sorted union of added, changed, and removed ARNs:
// the per-run changed-keys telemetry the #10878 audit compares against.
func (d ARNRecordDiff) ChangedKeys() []string {
	return unionSortedStrings(unionSortedStrings(d.Added, d.Changed), d.Removed)
}

// DiffARNRecords splits two ARN-to-payload-identity snapshots into the
// delta: keys only in next are added, keys in both with different identity
// are changed, keys only in prev are removed. Output slices are sorted so
// the signal is deterministic. A nil or empty prev means every key is
// added; identical snapshots diff to an empty, skippable delta.
func DiffARNRecords(prev, next map[string]string) ARNRecordDiff {
	var diff ARNRecordDiff
	for arn, nextID := range next {
		prevID, ok := prev[arn]
		switch {
		case !ok:
			diff.Added = append(diff.Added, arn)
		case prevID != nextID:
			diff.Changed = append(diff.Changed, arn)
		}
	}
	for arn := range prev {
		if _, ok := next[arn]; !ok {
			diff.Removed = append(diff.Removed, arn)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Changed)
	sort.Strings(diff.Removed)
	return diff
}

// SupplyChainImpactChangeSignal is the marked key for supply-chain impact
// (#10878 decision 2): the dirty (CVE, package, PURL) set changed since the
// last succeeded pass, in the winners dirty-log style (migration 272). A
// genuine pass derives its scoped stamp from this set so it scopes like the
// one-hop passes scoped_pass.go already keys; the until-stable expansion
// stays bounded to the stamp's reach.
type SupplyChainImpactChangeSignal struct {
	DirtyCVEIDs     []string
	DirtyPackageIDs []string
	DirtyPURLs      []string
}

// NeedsRederivation reports whether the dirty set names any work. An empty
// set means no genuine re-derivation: the pass skips.
func (s SupplyChainImpactChangeSignal) NeedsRederivation() bool {
	return len(s.DirtyCVEIDs) > 0 || len(s.DirtyPackageIDs) > 0 || len(s.DirtyPURLs) > 0
}

// DirtyKeys returns the sorted union of the dirty CVE, package, and PURL
// sets: the marked key the scoped stamp derives from.
func (s SupplyChainImpactChangeSignal) DirtyKeys() []string {
	return unionSortedStrings(s.DirtyCVEIDs, unionSortedStrings(s.DirtyPackageIDs, s.DirtyPURLs))
}

// WorkloadMaterializationChangeSignal is the input fingerprint for workload
// materialization (#10878 decision 2): the work unit's own generation plus
// ResolvedInputFingerprintSQL over the resolved rows it consumes
// (backfill.ResolvedInputFingerprintSQL), recorded after verified writes
// under the claim fence. The work unit stays whole-repo when the
// fingerprint moves: the win is runs avoided, not narrowing inside the repo.
type WorkloadMaterializationChangeSignal struct {
	RecordedValue   string
	RecordedVersion int
	CurrentValue    string
	CurrentVersion  int
}

// InputChanged reports whether the run must re-derive: the recorded row is
// missing, the fingerprint differs, or the logic version moved. It never
// reports skip-on-missing: absence re-opens.
func (s WorkloadMaterializationChangeSignal) InputChanged() bool {
	if s.RecordedValue == "" {
		return true
	}
	if s.RecordedVersion != s.CurrentVersion {
		return true
	}
	return s.RecordedValue != s.CurrentValue
}

// ContainerImageIdentityChangeSignal is the fingerprint plus marked key for
// container-image identity (#10878 decision 2). The fingerprint is the
// identity epoch the probe query already returns (count, max observed_at,
// ordered-set SHA-256 of active scope-generation pairs); it gates reopen.
// The marked key is the changed (scope:digest) pair set from the epoch
// diff, narrowing derivation. Fleet loads stay shared once per epoch
// version via the IdentityEpochCache singleflight.
type ContainerImageIdentityChangeSignal struct {
	EpochCount         int64
	EpochMaxObservedAt string
	EpochFingerprint   string
}

// EpochChanged reports whether the epoch moved between two observations:
// any of count, max observed_at, or the ordered-set fingerprint differs.
func (s ContainerImageIdentityChangeSignal) EpochChanged(prev ContainerImageIdentityChangeSignal) bool {
	return s.EpochCount != prev.EpochCount ||
		s.EpochMaxObservedAt != prev.EpochMaxObservedAt ||
		s.EpochFingerprint != prev.EpochFingerprint
}

// IdentityPairDiff is the marked-key split of two (scope:generation or
// scope:digest) pair sets: which pairs joined and which left.
type IdentityPairDiff struct {
	Added   []string
	Removed []string
}

// DiffIdentityPairs splits two pair sets into added (only in next) and
// removed (only in prev). Outputs are sorted. Identical sets diff empty.
func DiffIdentityPairs(prev, next []string) IdentityPairDiff {
	prevSet := make(map[string]struct{}, len(prev))
	for _, p := range prev {
		prevSet[p] = struct{}{}
	}
	nextSet := make(map[string]struct{}, len(next))
	for _, p := range next {
		nextSet[p] = struct{}{}
	}
	var diff IdentityPairDiff
	for _, p := range next {
		if _, ok := prevSet[p]; !ok {
			diff.Added = append(diff.Added, p)
		}
	}
	for _, p := range prev {
		if _, ok := nextSet[p]; !ok {
			diff.Removed = append(diff.Removed, p)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	return diff
}

// SearchDocumentsChangeSignal is the created marked key for search
// documents (#10878 decision 2): content tables hold the current snapshot
// keyed by repository, overwritten on re-index, so no generation diff
// exists. DirtyDocKeys captured at index-write time narrow the changed
// documents; RepoFingerprint (per-repo count, max indexed_at, ordered-set
// SHA-256 of document keys) skips unchanged repos.
type SearchDocumentsChangeSignal struct {
	RepoFingerprint     string
	RecordedFingerprint string
	RecordedVersion     int
	CurrentVersion      int
	DirtyDocKeys        []string
}

// NeedsReindex reports whether the repo must re-index: dirty keys exist, or
// no recorded fingerprint exists, or the fingerprint or logic version
// moved. Matching fingerprints with no dirty keys skip.
func (s SearchDocumentsChangeSignal) NeedsReindex() bool {
	if len(s.DirtyDocKeys) > 0 {
		return true
	}
	if s.RecordedFingerprint == "" {
		return true
	}
	if s.RecordedVersion != s.CurrentVersion {
		return true
	}
	return s.RepoFingerprint != s.RecordedFingerprint
}

// ComputeSearchRepoFingerprint renders the per-repository document
// fingerprint: the hex SHA-256 of count, max indexed_at, and the sorted
// document-key set joined by newlines. The digest is SHA-256, not MD5:
// OpenSSL FIPS rejects MD5 in production SQL (#6753). The empty set
// digests a fixed sentinel so the value is never empty and compares equal
// to itself.
func ComputeSearchRepoFingerprint(count int64, maxIndexedAt string, docKeys []string) string {
	keys := append([]string(nil), docKeys...)
	sort.Strings(keys)
	h := sha256.New()
	if count == 0 && maxIndexedAt == "" && len(keys) == 0 {
		h.Write([]byte("empty-search-repo"))
		return hex.EncodeToString(h.Sum(nil))
	}
	h.Write([]byte(strconv.FormatInt(count, 10)))
	h.Write([]byte("\n"))
	h.Write([]byte(maxIndexedAt))
	for _, k := range keys {
		h.Write([]byte("\n"))
		h.Write([]byte(k))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// unionSortedStrings returns the sorted union of two string sets.
func unionSortedStrings(a, b []string) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
