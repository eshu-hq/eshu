package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

func newTrigger(provider Provider, kind EventKind, deliveryID string, repo repositoryFields) (Trigger, error) {
	if repo.externalID == "" {
		return Trigger{}, errors.New("repository external id is required")
	}
	if repo.fullName == "" {
		return Trigger{}, errors.New("repository full name is required")
	}
	if repo.defaultBranch == "" {
		return Trigger{}, errors.New("repository default branch is required")
	}
	return Trigger{
		Provider:             provider,
		EventKind:            kind,
		DeliveryID:           strings.TrimSpace(deliveryID),
		RepositoryExternalID: repo.externalID,
		RepositoryFullName:   repo.fullName,
		DefaultBranch:        repo.defaultBranch,
	}, nil
}

func decideBranchTrigger(trigger Trigger) (Trigger, error) {
	if isTagRef(trigger.Ref) {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonTagRef
		return trigger, nil
	}

	branch, ok := branchFromRef(trigger.Ref)
	if !ok || branch != trigger.DefaultBranch {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonNonDefaultBranch
		return trigger, nil
	}
	if trigger.TargetSHA == "" {
		return Trigger{}, errors.New("target sha is required")
	}
	if isZeroSHA(trigger.TargetSHA) {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonDeletedBranch
		return trigger, nil
	}
	trigger.Decision = DecisionAccepted
	return trigger, nil
}

func decideDeletedRefTrigger(trigger Trigger) (Trigger, error) {
	if isTagRef(trigger.Ref) {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonTagRef
		return trigger, nil
	}

	branch, ok := branchFromRef(trigger.Ref)
	if !ok || branch != trigger.DefaultBranch {
		trigger.Decision = DecisionIgnored
		trigger.Reason = ReasonNonDefaultBranch
		return trigger, nil
	}
	trigger.Decision = DecisionIgnored
	trigger.Reason = ReasonDeletedBranch
	return trigger, nil
}

func isZeroSHA(sha string) bool {
	sha = strings.TrimSpace(sha)
	return sha != "" && strings.Trim(sha, "0") == ""
}

func branchFromRef(ref string) (string, bool) {
	return strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
}

func isTagRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "refs/tags/")
}

func branchRef(branch string) string {
	if branch == "" {
		return ""
	}
	return "refs/heads/" + branch
}

type repositoryFields struct {
	externalID    string
	fullName      string
	defaultBranch string
}

func rawScalar(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.String()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
