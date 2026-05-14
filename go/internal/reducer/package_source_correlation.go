package reducer

import (
	"net/url"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// PackageSourceCorrelationOutcome names the reducer decision for one package
// registry source hint. Exact and derived outcomes are still candidates; they
// do not authorize package ownership graph writes without stronger build or
// release provenance.
type PackageSourceCorrelationOutcome string

const (
	// PackageSourceCorrelationExact means the hint URL exactly matched one
	// active repository remote URL.
	PackageSourceCorrelationExact PackageSourceCorrelationOutcome = "exact"
	// PackageSourceCorrelationDerived means the hint URL matched one active
	// repository after git URL canonicalization such as SSH-to-HTTPS or .git
	// suffix removal.
	PackageSourceCorrelationDerived PackageSourceCorrelationOutcome = "derived"
	// PackageSourceCorrelationAmbiguous means more than one active repository
	// matched the same source hint.
	PackageSourceCorrelationAmbiguous PackageSourceCorrelationOutcome = "ambiguous"
	// PackageSourceCorrelationUnresolved means no repository matched the hint.
	PackageSourceCorrelationUnresolved PackageSourceCorrelationOutcome = "unresolved"
	// PackageSourceCorrelationStale means the hint matched only tombstoned
	// repository facts.
	PackageSourceCorrelationStale PackageSourceCorrelationOutcome = "stale"
	// PackageSourceCorrelationRejected means the hint cannot participate in
	// ownership correlation, such as homepage or generic project metadata.
	PackageSourceCorrelationRejected PackageSourceCorrelationOutcome = "rejected"
)

// PackageSourceCorrelationDecision records the bounded package-source
// correlation result before any canonical package ownership materialization.
type PackageSourceCorrelationDecision struct {
	PackageID              string
	VersionID              string
	HintKind               string
	SourceURL              string
	RepositoryID           string
	RepositoryName         string
	CandidateRepositoryIDs []string
	Outcome                PackageSourceCorrelationOutcome
	Reason                 string
	ProvenanceOnly         bool
	CanonicalWrites        int
}

type packageSourceHint struct {
	PackageID string
	VersionID string
	HintKind  string
	SourceURL string
}

type packageSourceRepository struct {
	RepositoryID   string
	RepositoryName string
	RemoteURL      string
	Tombstone      bool
}

// BuildPackageSourceCorrelationDecisions classifies package registry
// source_hint facts against repository facts for one reducer input set.
func BuildPackageSourceCorrelationDecisions(envelopes []facts.Envelope) []PackageSourceCorrelationDecision {
	hints := extractPackageSourceHints(envelopes)
	repositories := extractPackageSourceRepositories(envelopes)
	decisions := make([]PackageSourceCorrelationDecision, 0, len(hints))
	for _, hint := range hints {
		decisions = append(decisions, classifyPackageSourceHint(hint, repositories))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].PackageID != decisions[j].PackageID {
			return decisions[i].PackageID < decisions[j].PackageID
		}
		if decisions[i].SourceURL != decisions[j].SourceURL {
			return decisions[i].SourceURL < decisions[j].SourceURL
		}
		return decisions[i].HintKind < decisions[j].HintKind
	})
	return decisions
}

func extractPackageSourceHints(envelopes []facts.Envelope) []packageSourceHint {
	hints := make([]packageSourceHint, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistrySourceHintFactKind {
			continue
		}
		sourceURL := firstPackageSourceURL(
			payloadStr(envelope.Payload, "normalized_url"),
			payloadStr(envelope.Payload, "raw_url"),
		)
		hints = append(hints, packageSourceHint{
			PackageID: payloadStr(envelope.Payload, "package_id"),
			VersionID: payloadStr(envelope.Payload, "version_id"),
			HintKind:  strings.ToLower(payloadStr(envelope.Payload, "hint_kind")),
			SourceURL: sourceURL,
		})
	}
	return hints
}

func extractPackageSourceRepositories(envelopes []facts.Envelope) []packageSourceRepository {
	repositories := make([]packageSourceRepository, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindRepository {
			continue
		}
		repositoryID := firstPackageSourceURL(
			payloadStr(envelope.Payload, "graph_id"),
			payloadStr(envelope.Payload, "repo_id"),
		)
		if repositoryID == "" {
			continue
		}
		repositories = append(repositories, packageSourceRepository{
			RepositoryID:   repositoryID,
			RepositoryName: payloadStr(envelope.Payload, "name"),
			RemoteURL:      payloadStr(envelope.Payload, "remote_url"),
			Tombstone:      envelope.IsTombstone,
		})
	}
	sort.SliceStable(repositories, func(i, j int) bool {
		return repositories[i].RepositoryID < repositories[j].RepositoryID
	})
	return repositories
}

func classifyPackageSourceHint(
	hint packageSourceHint,
	repositories []packageSourceRepository,
) PackageSourceCorrelationDecision {
	decision := PackageSourceCorrelationDecision{
		PackageID:       hint.PackageID,
		VersionID:       hint.VersionID,
		HintKind:        hint.HintKind,
		SourceURL:       hint.SourceURL,
		ProvenanceOnly:  true,
		CanonicalWrites: 0,
	}
	if hint.PackageID == "" || hint.SourceURL == "" {
		decision.Outcome = PackageSourceCorrelationRejected
		decision.Reason = "source hint is missing package identity or URL"
		return decision
	}
	if hint.HintKind != "repository" {
		decision.Outcome = PackageSourceCorrelationRejected
		decision.Reason = "hint kind " + hint.HintKind + " is provenance-only and cannot prove repository ownership"
		return decision
	}

	activeMatches, staleMatches := matchPackageSourceRepositories(hint, repositories)
	switch len(activeMatches) {
	case 0:
		if len(staleMatches) > 0 {
			decision.Outcome = PackageSourceCorrelationStale
			decision.CandidateRepositoryIDs = packageSourceRepositoryIDs(staleMatches)
			decision.Reason = "source hint matched only tombstoned repository facts"
			return decision
		}
		decision.Outcome = PackageSourceCorrelationUnresolved
		decision.Reason = "source hint did not match any repository remote"
		return decision
	case 1:
		match := activeMatches[0]
		decision.RepositoryID = match.RepositoryID
		decision.RepositoryName = match.RepositoryName
		if exactPackageSourceURLMatch(hint.SourceURL, match.RemoteURL) {
			decision.Outcome = PackageSourceCorrelationExact
			decision.Reason = "source hint matches repository remote exactly"
			return decision
		}
		decision.Outcome = PackageSourceCorrelationDerived
		decision.Reason = "source hint matches repository remote after git URL canonicalization"
		return decision
	default:
		decision.Outcome = PackageSourceCorrelationAmbiguous
		decision.CandidateRepositoryIDs = packageSourceRepositoryIDs(activeMatches)
		decision.Reason = "source hint matches multiple active repository remotes"
		return decision
	}
}

func matchPackageSourceRepositories(
	hint packageSourceHint,
	repositories []packageSourceRepository,
) ([]packageSourceRepository, []packageSourceRepository) {
	hintKey := canonicalPackageSourceURLKey(hint.SourceURL)
	if hintKey == "" {
		return nil, nil
	}
	var activeMatches []packageSourceRepository
	var staleMatches []packageSourceRepository
	for _, repository := range repositories {
		if canonicalPackageSourceURLKey(repository.RemoteURL) != hintKey {
			continue
		}
		if repository.Tombstone {
			staleMatches = append(staleMatches, repository)
			continue
		}
		activeMatches = append(activeMatches, repository)
	}
	return activeMatches, staleMatches
}

func packageSourceRepositoryIDs(repositories []packageSourceRepository) []string {
	ids := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		ids = append(ids, repository.RepositoryID)
	}
	sort.Strings(ids)
	return ids
}

func exactPackageSourceURLMatch(left string, right string) bool {
	return normalizePackageSourceExactURL(left) == normalizePackageSourceExactURL(right)
}

func normalizePackageSourceExactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(trimmed, "/")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.User = nil
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func canonicalPackageSourceURLKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "git+")
	if scpKey := canonicalPackageSourceSCPKey(trimmed); scpKey != "" {
		return scpKey
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	if at := strings.LastIndex(host, "@"); at >= 0 && at < len(host)-1 {
		host = host[at+1:]
	}
	pathValue := strings.Trim(parsed.EscapedPath(), "/")
	pathValue = strings.TrimSuffix(pathValue, ".git")
	if pathValue == "" {
		return ""
	}
	return host + "/" + strings.ToLower(pathValue)
}

func canonicalPackageSourceSCPKey(raw string) string {
	if strings.Contains(raw, "://") {
		return ""
	}
	beforeColon, afterColon, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(afterColon) == "" {
		return ""
	}
	host := beforeColon
	if at := strings.LastIndex(host, "@"); at >= 0 && at < len(host)-1 {
		host = host[at+1:]
	}
	host = strings.ToLower(strings.TrimSpace(host))
	pathValue := strings.Trim(strings.TrimSpace(afterColon), "/")
	pathValue = strings.TrimSuffix(pathValue, ".git")
	if host == "" || pathValue == "" {
		return ""
	}
	return host + "/" + strings.ToLower(pathValue)
}

func firstPackageSourceURL(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
