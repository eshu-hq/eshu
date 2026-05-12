package tfstateruntime

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// generationForCandidate derives the IngestionScope and ScopeGeneration that
// represent one Terraform-state snapshot for the resolved candidate and
// already-opened source identity. Callers use the returned pair as the
// canonical identity for fact emission and claim-match verification.
func generationForCandidate(
	candidate terraformstate.DiscoveryCandidate,
	sourceKey terraformstate.StateKey,
	identity terraformstate.SnapshotIdentity,
	observedAt time.Time,
) (scope.IngestionScope, scope.ScopeGeneration, error) {
	scopeValue, err := scopeForCandidate(candidate, sourceKey)
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	generationValue, err := scope.NewTerraformStateSnapshotGeneration(
		scopeValue.ScopeID,
		identity.Serial,
		identity.Lineage,
		observedAt,
	)
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	return scopeValue, generationValue, nil
}

// scopeForCandidate builds the IngestionScope for a candidate using the
// already-verified source key. Repo metadata is preserved in scope metadata so
// downstream consumers can correlate the snapshot back to its discovering repo
// without re-resolving.
func scopeForCandidate(
	candidate terraformstate.DiscoveryCandidate,
	sourceKey terraformstate.StateKey,
) (scope.IngestionScope, error) {
	metadata := map[string]string{}
	if repoID := strings.TrimSpace(candidate.RepoID); repoID != "" {
		metadata["repo_id"] = repoID
	}
	return scope.NewTerraformStateSnapshotScope(
		strings.TrimSpace(candidate.RepoID),
		string(sourceKey.BackendKind),
		sourceKey.Locator,
		metadata,
	)
}

// ensureSourceIdentity rejects a source whose post-open identity disagrees
// with the candidate the resolver returned. This guards against locator
// mutation between discovery and read (e.g., S3 key rewrite mid-resolve).
func ensureSourceIdentity(expected terraformstate.StateKey, actual terraformstate.StateKey) error {
	if expected == actual {
		return nil
	}
	return fmt.Errorf("terraform state source identity mismatch for %s candidate", expected.BackendKind)
}

// claimMatchesCandidate is the cheap pre-open check: the claim's scope id
// matches the candidate's scope, and the planning-id form of the work item
// matches the candidate's identity. Returns true when the runtime should
// proceed to open the source.
func claimMatchesCandidate(item workflow.WorkItem, scopeValue scope.IngestionScope, candidateID string) bool {
	if item.ScopeID != scopeValue.ScopeID {
		return false
	}
	if !usesCandidatePlanningID(item) {
		return true
	}
	return item.GenerationID == candidateID && item.SourceRunID == candidateID
}

// claimMatchesCollected is the post-open check: the resolved generation id
// must match the claim's expectation. Two forms exist — planning-id work
// items match the candidate id; resolved-generation work items match the
// observed generation id. The function disambiguates by inspecting the work
// item's id shape.
func claimMatchesCollected(
	item workflow.WorkItem,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	candidateID string,
) bool {
	if item.ScopeID != scopeValue.ScopeID {
		return false
	}
	if usesCandidatePlanningID(item) {
		return item.GenerationID == candidateID && item.SourceRunID == candidateID
	}
	return item.GenerationID == generationValue.GenerationID && item.SourceRunID == generationValue.GenerationID
}

// usesCandidatePlanningID returns true when the work item still carries the
// pre-open planning identifier (issued by the workflow coordinator) rather
// than a resolved-generation id. The two forms move through claim matching
// down different branches.
func usesCandidatePlanningID(item workflow.WorkItem) bool {
	return terraformstate.IsCandidatePlanningID(item.GenerationID) ||
		terraformstate.IsCandidatePlanningID(item.SourceRunID)
}

// sourceWarningsForCandidate emits a state_in_vcs warning when the candidate
// was approved out of repo-local discovery. The warning fact lets downstream
// auditors trace which Terraform-state files were pulled out of source
// control on purpose.
func sourceWarningsForCandidate(candidate terraformstate.DiscoveryCandidate) []terraformstate.SourceWarning {
	if !candidate.StateInVCS {
		return nil
	}
	return []terraformstate.SourceWarning{{
		WarningKind: "state_in_vcs",
		Reason:      "terraform state file was discovered in git and explicitly approved for ingestion",
		Source:      string(candidate.Source),
	}}
}

// firstTime returns the first non-zero time in values, or time.Now().UTC()
// when every value is zero. Used to pick a stable observed-at for facts when
// source metadata may or may not supply one.
func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Now().UTC()
}

// closeReader closes reader when non-nil and swallows errors. Used in defer
// blocks where a close error would shadow the real read/parse error.
func closeReader(reader io.Closer) {
	if reader != nil {
		_ = reader.Close()
	}
}

// parseS3Locator splits an s3://bucket/key locator into its components. The
// SourceFactory builds an S3 client request from these and rejects malformed
// locators before any AWS API call.
func parseS3Locator(locator string) (string, string, error) {
	rest, ok := strings.CutPrefix(locator, "s3://")
	if !ok {
		return "", "", fmt.Errorf("s3 state locator must start with s3://")
	}
	bucket, key, ok := strings.Cut(rest, "/")
	if !ok || strings.TrimSpace(bucket) == "" || strings.TrimSpace(key) == "" {
		return "", "", fmt.Errorf("s3 state locator must include bucket and key")
	}
	return bucket, key, nil
}

// sourceFailure wraps an arbitrary underlying error in a sourceError that
// redacts the locator and stamps the failing action ("build" or "open"). The
// returned error preserves the original cause through errors.Unwrap.
func sourceFailure(action string, state terraformstate.StateKey, err error) error {
	var existing sourceError
	if errors.As(err, &existing) {
		return err
	}
	message := err.Error()
	if locator := strings.TrimSpace(state.Locator); locator != "" {
		message = strings.ReplaceAll(message, locator, "<redacted>")
	}
	return sourceError{
		action:      action,
		backendKind: state.BackendKind,
		message:     message,
		cause:       err,
	}
}

// sourceError is the typed error wrapper the collector returns for build/open
// failures. The action and backend kind let upstream metrics classify the
// failure without inspecting the underlying error text.
type sourceError struct {
	action      string
	backendKind terraformstate.BackendKind
	message     string
	cause       error
}

// Error implements the error interface.
func (e sourceError) Error() string {
	return fmt.Sprintf("%s terraform state %s source: %s", e.action, e.backendKind, e.message)
}

// Unwrap preserves the underlying cause so callers can match against typed
// errors with errors.Is and errors.As.
func (e sourceError) Unwrap() error {
	return e.cause
}
