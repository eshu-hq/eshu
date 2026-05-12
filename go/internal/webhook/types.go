package webhook

import "time"

// Provider names the source control provider that emitted a webhook.
type Provider string

const (
	// ProviderGitHub identifies webhook events emitted by GitHub.
	ProviderGitHub Provider = "github"
	// ProviderGitLab identifies webhook events emitted by GitLab.
	ProviderGitLab Provider = "gitlab"
	// ProviderBitbucket identifies webhook events emitted by Bitbucket Cloud.
	ProviderBitbucket Provider = "bitbucket"
)

// EventKind names the normalized event category that can trigger refresh work.
type EventKind string

const (
	// EventKindPush represents a push to a repository branch.
	EventKindPush EventKind = "push"
	// EventKindPullRequestMerged represents a pull request merge.
	EventKindPullRequestMerged EventKind = "pull_request_merged"
	// EventKindMergeRequestMerged represents a GitLab merge request merge.
	EventKindMergeRequestMerged EventKind = "merge_request_merged"
)

// Decision records whether a provider event should create refresh work.
type Decision string

const (
	// DecisionAccepted means the event should create or deduplicate refresh work.
	DecisionAccepted Decision = "accepted"
	// DecisionIgnored means the event was valid but should not refresh a repo.
	DecisionIgnored Decision = "ignored"
)

// DecisionReason explains why a valid provider event did not trigger refresh.
type DecisionReason string

const (
	// ReasonNonDefaultBranch means the event targeted a non-default branch.
	ReasonNonDefaultBranch DecisionReason = "non_default_branch"
	// ReasonTagRef means the event targeted a tag rather than a branch.
	ReasonTagRef DecisionReason = "tag_ref"
	// ReasonPullRequestNotMerged means a provider pull request was not merged.
	ReasonPullRequestNotMerged DecisionReason = "pull_request_not_merged"
	// ReasonMergeRequestNotMerged means a GitLab merge request was not merged.
	ReasonMergeRequestNotMerged DecisionReason = "merge_request_not_merged"
	// ReasonMissingMergeCommit means a merge event lacked the merge commit SHA.
	ReasonMissingMergeCommit DecisionReason = "missing_merge_commit"
	// ReasonDeletedBranch means a branch push deleted the branch instead of refreshing it.
	ReasonDeletedBranch DecisionReason = "deleted_branch"
)

// Trigger is the provider-neutral refresh trigger decision.
//
// RepositoryExternalID and DeliveryID form provider-scoped idempotency inputs
// for later durable storage. TargetSHA is the commit the collector should make
// visible through normal git sync, snapshotting, fact emission, and projection.
type Trigger struct {
	Provider             Provider
	EventKind            EventKind
	Decision             Decision
	Reason               DecisionReason
	DeliveryID           string
	RepositoryExternalID string
	RepositoryFullName   string
	DefaultBranch        string
	Ref                  string
	BeforeSHA            string
	TargetSHA            string
	Action               string
	Sender               string
}

// TriggerStatus describes the durable intake and handoff lifecycle for one
// normalized webhook delivery.
type TriggerStatus string

const (
	// TriggerStatusQueued means the accepted trigger is waiting for git refresh.
	TriggerStatusQueued TriggerStatus = "queued"
	// TriggerStatusIgnored means the trigger was valid but intentionally skipped.
	TriggerStatusIgnored TriggerStatus = "ignored"
	// TriggerStatusClaimed means a collector compatibility handoff claimed it.
	TriggerStatusClaimed TriggerStatus = "claimed"
	// TriggerStatusHandedOff means a collector selected the repository refresh.
	TriggerStatusHandedOff TriggerStatus = "handed_off"
	// TriggerStatusFailed means durable handoff failed and needs attention.
	TriggerStatusFailed TriggerStatus = "failed"
)

// StoredTrigger is the durable representation of a normalized webhook trigger.
type StoredTrigger struct {
	Trigger
	TriggerID      string
	DeliveryKey    string
	RefreshKey     string
	Status         TriggerStatus
	DuplicateCount int
	ReceivedAt     time.Time
	UpdatedAt      time.Time
}
