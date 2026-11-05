// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	// GoReachabilityScopePrefix starts every Go reachability scope id. The rest
	// of the id is the canonical repository id, so no URL reaches a scope id,
	// and through it a log, label, or status row.
	GoReachabilityScopePrefix = "go-reachability://"
	// InstallationOwnerKeyPrefix starts an ESHU_GITHUB_APP_INSTALLATIONS key
	// that selects an installation by repository host and owner rather than by
	// exact scope id. The prefix keeps the two key forms from colliding.
	InstallationOwnerKeyPrefix = "github-owner:"
	// GoModulePageSize is the most candidate repositories one reconcile pass
	// reads, and so the most Go repositories one pass plans.
	GoModulePageSize = 256
	// EcosystemGo is the claim ecosystem of every Go reachability work item.
	EcosystemGo = "go"
)

var (
	canonicalRepositoryIDPattern = regexp.MustCompile(`^repository:r_[0-9a-f]{8}$`)
	// A full object name: SHA-1 or SHA-256. A branch or abbreviated commit is
	// not an observed commit the scanner can pin.
	sourceCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// GoReachabilityScopeID returns the scope id for one canonical repository id.
// Anything else is refused, so a URL or local path cannot reach the id.
func GoReachabilityScopeID(repositoryID string) (string, error) {
	if !canonicalRepositoryIDPattern.MatchString(repositoryID) {
		return "", errors.New("go reachability scope requires a canonical repository id")
	}
	return GoReachabilityScopePrefix + repositoryID, nil
}

// ParseGoReachabilityScopeID returns the repository id inside a Go reachability
// scope id. It reports false for every other scope id, which is how the host
// keeps other claims free of any run-row read.
func ParseGoReachabilityScopeID(scopeID string) (string, bool) {
	repositoryID, ok := strings.CutPrefix(scopeID, GoReachabilityScopePrefix)
	if !ok || !canonicalRepositoryIDPattern.MatchString(repositoryID) {
		return "", false
	}
	return repositoryID, true
}

// GoReachabilityTarget is one derived target as the coordinator persists it in
// a run's requested_scope_set and the host delivers it on the claim. The JSON
// names are the transport contract with the eshu-scanner-worker component.
type GoReachabilityTarget struct {
	ScopeID          string `json:"scope_id"`
	TargetKind       string `json:"target_kind"`
	RepositoryID     string `json:"repository_id"`
	SourceCommitSHA  string `json:"source_commit_sha"`
	CloneURL         string `json:"clone_url"`
	AcceptanceUnitID string `json:"acceptance_unit_id"`
}

// Validate reports the first defect in a target. Errors name the defect and
// never the value: a remote URL may be private, and a malformed one may hold a
// credential.
func (t GoReachabilityTarget) Validate() error {
	repositoryID, ok := ParseGoReachabilityScopeID(t.ScopeID)
	if !ok {
		return errors.New("go reachability target scope id is not canonical")
	}
	if t.RepositoryID != repositoryID {
		return errors.New("go reachability target repository id does not match its scope id")
	}
	if t.AcceptanceUnitID != t.ScopeID {
		return errors.New("go reachability target acceptance unit does not match its scope id")
	}
	if t.TargetKind != string(TargetRepository) {
		return errors.New("go reachability target kind must be repository")
	}
	if !sourceCommitPattern.MatchString(t.SourceCommitSHA) {
		return errors.New("go reachability target source commit is not a full object name")
	}
	if !ValidCloneURL(t.CloneURL) {
		return errors.New("go reachability target clone url is not a canonical https remote")
	}
	return nil
}

// ValidCloneURL reports whether a stored remote is the canonical
// https://host/owner/repo form the git collector normalizes to. Remotes the
// normalizer passes through unchanged (local paths, file URLs) fail, and so
// does any URL carrying userinfo, a port, a query, or a fragment, because
// normalizing would change it.
func ValidCloneURL(raw string) bool {
	rest, ok := strings.CutPrefix(raw, "https://")
	if !ok || repositoryidentity.NormalizeRemoteURL(raw) != raw {
		return false
	}
	segments := strings.Split(rest, "/")
	if len(segments) < 3 {
		return false
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// InstallationKey returns the ESHU_GITHUB_APP_INSTALLATIONS key that selects an
// installation for this target: the host and owner of its clone URL. The host
// is part of the key on purpose. The token goes to the same URL the owner came
// from, so an owner-only key would mint a mapped owner's token for a look-alike
// remote on another host. Call it only on a validated target.
func (t GoReachabilityTarget) InstallationKey() string {
	rest := strings.TrimPrefix(t.CloneURL, "https://")
	segments := strings.SplitN(rest, "/", 3)
	if len(segments) < 2 {
		return ""
	}
	return InstallationOwnerKeyPrefix + segments[0] + "/" + segments[1]
}

// RequestedScopeSet is the requested_scope_set document for one planned page.
type RequestedScopeSet struct {
	CollectorInstanceID string                 `json:"collector_instance_id"`
	Analyzer            string                 `json:"analyzer"`
	Targets             []GoReachabilityTarget `json:"targets"`
}

// ResolveGoReachabilityTarget returns the validated entry for one scope from a
// run's requested_scope_set. A missing, ambiguous, or invalid entry is an error,
// never a guessed target: retrying cannot repair the row.
func ResolveGoReachabilityTarget(requestedScopeSetJSON, scopeID string) (GoReachabilityTarget, error) {
	var document RequestedScopeSet
	if err := json.Unmarshal([]byte(requestedScopeSetJSON), &document); err != nil {
		return GoReachabilityTarget{}, errors.New("requested scope set is not parsable")
	}
	var matched *GoReachabilityTarget
	for i := range document.Targets {
		if document.Targets[i].ScopeID != scopeID {
			continue
		}
		if matched != nil {
			return GoReachabilityTarget{}, errors.New("requested scope set entry is ambiguous")
		}
		matched = &document.Targets[i]
	}
	if matched == nil {
		return GoReachabilityTarget{}, errors.New("requested scope set has no entry for the scope")
	}
	if err := matched.Validate(); err != nil {
		return GoReachabilityTarget{}, fmt.Errorf("requested scope set entry: %w", err)
	}
	return *matched, nil
}

// GoModuleRepository is one repository to scan: its active generation holds a
// go.mod, or its go-reachability scope still holds Go evidence to retract.
type GoModuleRepository struct {
	RepositoryID    string
	RemoteURL       string
	SourceCommitSHA string
	// HasGoEvidence reports that the repository's go-reachability scope still
	// holds Go evidence. A non-canonical remote on such a repository should be
	// impossible, so the planner counts it in its own skip reason as an alarm.
	HasGoEvidence bool
}

// GoModuleRepositoryFilter selects one rotation page of repository candidates.
type GoModuleRepositoryFilter struct {
	// RunID is the run this bucket's page plans under. A reader returns
	// AlreadyPlanned without reading candidates when the run exists, so a planned
	// page stays fixed for its bucket and a repeat pass costs one primary-key probe.
	// A page that planned no run has no such row and is read again.
	RunID string
	// RotationBucket picks the page: bucket mod the slot count.
	RotationBucket int64
	// PageSize is the candidate count per page, at most GoModulePageSize.
	PageSize int
}

// GoModuleRepositoryPage is one rotation page.
type GoModuleRepositoryPage struct {
	AlreadyPlanned bool
	// Repositories holds the page's candidates to scan: those with a go.mod, and
	// those whose go-reachability scope still holds Go evidence.
	Repositories []GoModuleRepository
	// Candidates counts the repositories the page examined.
	Candidates int
	// EvidenceOnly counts the Repositories planned only because their scope
	// still holds Go evidence: each lost its go.mod and is scanned once more so a
	// trusted no_go_modules generation can retract that evidence.
	EvidenceOnly int
	// TotalCandidates counts every repository scope with an active generation.
	TotalCandidates int
	Slot            int
	SlotCount       int
}

// GoModulePageWindow returns the slot a rotation bucket selects and the slot
// count for a fleet. The SQL read applies the same arithmetic. An empty fleet
// has one empty slot, and a negative bucket reads as bucket zero.
func GoModulePageWindow(bucket int64, total, pageSize int) (slot, slotCount int) {
	if total <= 0 || pageSize <= 0 {
		return 0, 1
	}
	slotCount = (total + pageSize - 1) / pageSize
	return int(max(bucket, 0) % int64(slotCount)), slotCount
}
