package terraformstate

import "strings"

// LocalStateCandidateMode controls repo-local state candidate promotion.
type LocalStateCandidateMode string

const (
	// LocalStateCandidateModeDiscoverOnly keeps repo-local state as metadata.
	LocalStateCandidateModeDiscoverOnly LocalStateCandidateMode = "discover_only"
	// LocalStateCandidateModeApproved allows explicitly approved repo-local reads.
	LocalStateCandidateModeApproved LocalStateCandidateMode = "approved_candidates"
)

// LocalStateCandidatePolicy carries operator approval for repo-local state.
type LocalStateCandidatePolicy struct {
	Mode     LocalStateCandidateMode
	Approved []LocalStateCandidateRef
	Ignored  []LocalStateCandidateRef
}

// LocalStateCandidateRef identifies a repo-local state candidate without raw
// content or an absolute local path.
type LocalStateCandidateRef struct {
	RepoID        string
	RelativePath  string
	TargetScopeID string
}

func (p LocalStateCandidatePolicy) approvedMode() bool {
	return p.Mode == LocalStateCandidateModeApproved
}

func localCandidateRefsContain(refs []LocalStateCandidateRef, target LocalStateCandidateRef) bool {
	target = target.normalized()
	for _, ref := range refs {
		ref = ref.normalized()
		if ref.RepoID == target.RepoID && ref.RelativePath == target.RelativePath {
			return true
		}
	}
	return false
}

func localCandidateRefForCandidate(
	refs []LocalStateCandidateRef,
	target LocalStateCandidateRef,
) (LocalStateCandidateRef, bool) {
	target = target.normalized()
	for _, ref := range refs {
		ref = ref.normalized()
		if ref.RepoID == target.RepoID && ref.RelativePath == target.RelativePath {
			return ref, true
		}
	}
	return LocalStateCandidateRef{}, false
}

func normalizedLocalCandidateRefs(refs []LocalStateCandidateRef) []LocalStateCandidateRef {
	normalized := make([]LocalStateCandidateRef, 0, len(refs))
	seen := map[LocalStateCandidateRef]struct{}{}
	for _, ref := range refs {
		ref = ref.normalized()
		if ref.RepoID == "" || ref.RelativePath == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		normalized = append(normalized, ref)
	}
	return normalized
}

func (r LocalStateCandidateRef) normalized() LocalStateCandidateRef {
	return LocalStateCandidateRef{
		RepoID:        strings.TrimSpace(r.RepoID),
		RelativePath:  cleanRelativePath(r.RelativePath),
		TargetScopeID: strings.TrimSpace(r.TargetScopeID),
	}
}

func cleanRelativePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = strings.TrimPrefix(path, "./")
	return strings.Trim(path, "/")
}
