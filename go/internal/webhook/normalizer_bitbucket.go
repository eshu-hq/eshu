package webhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	bitbucketEventPush                 = "repo:push"
	bitbucketEventPullRequestFulfilled = "pullrequest:fulfilled"
)

// NormalizeBitbucket converts a verified Bitbucket Cloud event payload into a
// refresh trigger decision.
func NormalizeBitbucket(event string, deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	switch strings.TrimSpace(event) {
	case bitbucketEventPush:
		return normalizeBitbucketPush(deliveryID, payload, defaultBranchFallback)
	case bitbucketEventPullRequestFulfilled:
		return normalizeBitbucketPullRequestFulfilled(deliveryID, payload, defaultBranchFallback)
	default:
		return Trigger{}, fmt.Errorf("unsupported bitbucket webhook event %q", event)
	}
}

func normalizeBitbucketPush(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event bitbucketPushPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	var firstIgnored Trigger
	for _, change := range event.Push.Changes {
		trigger, err := newTrigger(ProviderBitbucket, EventKindPush, deliveryID, event.Repository.common(defaultBranchFallback))
		if err != nil {
			return Trigger{}, err
		}
		trigger.Sender = strings.TrimSpace(event.Actor.Nickname)
		decision, err := normalizeBitbucketPushChange(trigger, change)
		if err != nil {
			return Trigger{}, err
		}
		if decision.Decision == DecisionAccepted {
			return decision, nil
		}
		if firstIgnored.Decision == "" {
			firstIgnored = decision
		}
	}
	if firstIgnored.Decision != "" {
		return firstIgnored, nil
	}
	return Trigger{}, fmt.Errorf("bitbucket push event did not include changes")
}

func normalizeBitbucketPushChange(trigger Trigger, change bitbucketPushChange) (Trigger, error) {
	if change.New == nil {
		trigger.Ref = bitbucketRef(change.Old)
		return decideDeletedRefTrigger(trigger)
	}
	trigger.Ref = bitbucketRef(change.New)
	trigger.TargetSHA = strings.TrimSpace(change.New.Target.Hash)
	if change.Old != nil {
		trigger.BeforeSHA = strings.TrimSpace(change.Old.Target.Hash)
	}
	return decideBranchTrigger(trigger)
}

func normalizeBitbucketPullRequestFulfilled(deliveryID string, payload []byte, defaultBranchFallback string) (Trigger, error) {
	var event bitbucketPullRequestPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return Trigger{}, err
	}

	trigger, err := newTrigger(ProviderBitbucket, EventKindPullRequestMerged, deliveryID, event.Repository.common(defaultBranchFallback))
	if err != nil {
		return Trigger{}, err
	}
	trigger.Action = "fulfilled"
	trigger.Ref = branchRef(strings.TrimSpace(event.PullRequest.Destination.Branch.Name))
	if event.PullRequest.MergeCommit != nil {
		trigger.TargetSHA = strings.TrimSpace(event.PullRequest.MergeCommit.Hash)
	}
	trigger.Sender = strings.TrimSpace(event.Actor.Nickname)

	if state := strings.TrimSpace(event.PullRequest.State); state != "" && !strings.EqualFold(state, "MERGED") {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonPullRequestNotMerged
		return trigger, nil
	}
	if trigger.TargetSHA == "" {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonMissingMergeCommit
		return trigger, nil
	}
	return decideBranchTrigger(trigger)
}

func bitbucketRef(ref *bitbucketRefChange) string {
	if ref == nil {
		return ""
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(ref.Type)) {
	case "tag":
		return "refs/tags/" + name
	default:
		return branchRef(name)
	}
}

type bitbucketRepository struct {
	UUID       string `json:"uuid"`
	FullName   string `json:"full_name"`
	MainBranch struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
}

func (repo bitbucketRepository) common(defaultBranchFallback string) repositoryFields {
	return repositoryFields{
		externalID:    strings.TrimSpace(repo.UUID),
		fullName:      strings.TrimSpace(repo.FullName),
		defaultBranch: firstNonEmpty(repo.MainBranch.Name, defaultBranchFallback),
	}
}

type bitbucketActor struct {
	Nickname string `json:"nickname"`
}

type bitbucketTarget struct {
	Hash string `json:"hash"`
}

type bitbucketRefChange struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Target bitbucketTarget `json:"target"`
}

type bitbucketPushPayload struct {
	Actor      bitbucketActor      `json:"actor"`
	Repository bitbucketRepository `json:"repository"`
	Push       struct {
		Changes []bitbucketPushChange `json:"changes"`
	} `json:"push"`
}

type bitbucketPushChange struct {
	Old *bitbucketRefChange `json:"old"`
	New *bitbucketRefChange `json:"new"`
}

type bitbucketPullRequestPayload struct {
	Actor       bitbucketActor      `json:"actor"`
	Repository  bitbucketRepository `json:"repository"`
	PullRequest struct {
		State       string `json:"state"`
		Destination struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
		} `json:"destination"`
		MergeCommit *bitbucketTarget `json:"merge_commit"`
	} `json:"pullrequest"`
}
