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
	RepoID       string
	RelativePath string
}

func (p LocalStateCandidatePolicy) approvedMode() bool {
	return p.Mode == LocalStateCandidateModeApproved
}

func localCandidateRefsContain(refs []LocalStateCandidateRef, target LocalStateCandidateRef) bool {
	target = target.normalized()
	for _, ref := range refs {
		if ref.normalized() == target {
			return true
		}
	}
	return false
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
		RepoID:       strings.TrimSpace(r.RepoID),
		RelativePath: cleanRelativePath(r.RelativePath),
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
