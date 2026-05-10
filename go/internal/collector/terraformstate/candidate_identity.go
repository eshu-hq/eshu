package terraformstate

import (
	"fmt"
	"strings"
)

const candidatePlanningIDPrefix = "terraform_state_candidate:"

// CandidatePlanningID returns the durable planning identity for one exact
// Terraform state candidate before the state serial and lineage are known.
func CandidatePlanningID(candidate DiscoveryCandidate) (string, error) {
	if err := candidate.Validate(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s:%s", candidatePlanningIDPrefix, candidate.State.BackendKind, locatorHash(candidate.State)), nil
}

// IsCandidatePlanningID reports whether id uses the pre-read Terraform-state
// candidate planning identity namespace.
func IsCandidatePlanningID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), candidatePlanningIDPrefix)
}
