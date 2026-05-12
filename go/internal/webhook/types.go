package webhook

// Provider names the source control provider that emitted a webhook.
type Provider string

const (
	// ProviderGitHub identifies webhook events emitted by GitHub.
	ProviderGitHub Provider = "github"
	// ProviderGitLab identifies webhook events emitted by GitLab.
	ProviderGitLab Provider = "gitlab"
)

// EventKind names the normalized event category that can trigger refresh work.
type EventKind string

const (
	// EventKindPush represents a push to a repository branch.
	EventKindPush EventKind = "push"
	// EventKindPullRequestMerged represents a GitHub pull request merge.
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
	// ReasonPullRequestNotMerged means a GitHub pull request was not merged.
	ReasonPullRequestNotMerged DecisionReason = "pull_request_not_merged"
	// ReasonMergeRequestNotMerged means a GitLab merge request was not merged.
	ReasonMergeRequestNotMerged DecisionReason = "merge_request_not_merged"
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
