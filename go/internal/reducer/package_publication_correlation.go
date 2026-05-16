package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// PackagePublicationDecision records one package-version publication
// correlation. Source-hint-only publication evidence stays provenance-only
// until build, release, or CI provenance can admit a canonical publication edge.
type PackagePublicationDecision struct {
	PackageID              string
	VersionID              string
	Version                string
	PublishedAt            string
	SourceURL              string
	SourceHintFactID       string
	SourceHintKind         string
	SourceHintVersionID    string
	RepositoryID           string
	RepositoryName         string
	CandidateRepositoryIDs []string
	Outcome                PackageSourceCorrelationOutcome
	Reason                 string
	ProvenanceOnly         bool
	CanonicalWrites        int
	EvidenceFactIDs        []string
}

type packageRegistryVersionIdentity struct {
	FactID      string
	PackageID   string
	VersionID   string
	Version     string
	PublishedAt string
}

// BuildPackagePublicationDecisions correlates package versions to source
// repository hints. It reports publication evidence without promoting package
// ownership or canonical publication writes from registry metadata alone.
func BuildPackagePublicationDecisions(envelopes []facts.Envelope) []PackagePublicationDecision {
	versions := extractPackageRegistryVersions(envelopes)
	hints := indexPackagePublicationHints(extractPackageSourceHints(envelopes))
	repositories := extractPackageSourceRepositories(envelopes)
	decisions := make([]PackagePublicationDecision, 0)
	for _, version := range versions {
		for _, hint := range hints.forVersion(version) {
			sourceDecision := classifyPackageSourceHint(hint, repositories)
			decisions = append(decisions, PackagePublicationDecision{
				PackageID:              version.PackageID,
				VersionID:              version.VersionID,
				Version:                version.Version,
				PublishedAt:            version.PublishedAt,
				SourceURL:              sourceDecision.SourceURL,
				SourceHintFactID:       hint.FactID,
				SourceHintKind:         hint.HintKind,
				SourceHintVersionID:    hint.VersionID,
				RepositoryID:           sourceDecision.RepositoryID,
				RepositoryName:         sourceDecision.RepositoryName,
				CandidateRepositoryIDs: uniqueSortedStrings(sourceDecision.CandidateRepositoryIDs),
				Outcome:                sourceDecision.Outcome,
				Reason:                 sourceDecision.Reason,
				ProvenanceOnly:         true,
				CanonicalWrites:        0,
				EvidenceFactIDs:        uniqueSortedStrings(append(sourceDecision.EvidenceFactIDs, version.FactID)),
			})
		}
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].PackageID != decisions[j].PackageID {
			return decisions[i].PackageID < decisions[j].PackageID
		}
		if decisions[i].VersionID != decisions[j].VersionID {
			return decisions[i].VersionID < decisions[j].VersionID
		}
		if decisions[i].SourceURL != decisions[j].SourceURL {
			return decisions[i].SourceURL < decisions[j].SourceURL
		}
		if decisions[i].SourceHintVersionID != decisions[j].SourceHintVersionID {
			return decisions[i].SourceHintVersionID < decisions[j].SourceHintVersionID
		}
		if decisions[i].SourceHintKind != decisions[j].SourceHintKind {
			return decisions[i].SourceHintKind < decisions[j].SourceHintKind
		}
		return decisions[i].SourceHintFactID < decisions[j].SourceHintFactID
	})
	return decisions
}

type packagePublicationHintIndex struct {
	byPackage map[string][]packageSourceHint
	byVersion map[string][]packageSourceHint
}

func indexPackagePublicationHints(hints []packageSourceHint) packagePublicationHintIndex {
	index := packagePublicationHintIndex{
		byPackage: make(map[string][]packageSourceHint),
		byVersion: make(map[string][]packageSourceHint),
	}
	for _, hint := range hints {
		if hint.PackageID == "" {
			continue
		}
		if hint.VersionID == "" {
			index.byPackage[hint.PackageID] = append(index.byPackage[hint.PackageID], hint)
			continue
		}
		index.byVersion[hint.VersionID] = append(index.byVersion[hint.VersionID], hint)
	}
	return index
}

func (i packagePublicationHintIndex) forVersion(version packageRegistryVersionIdentity) []packageSourceHint {
	if version.PackageID == "" || version.VersionID == "" {
		return nil
	}
	hints := make([]packageSourceHint, 0, len(i.byPackage[version.PackageID])+len(i.byVersion[version.VersionID]))
	hints = append(hints, i.byPackage[version.PackageID]...)
	hints = append(hints, i.byVersion[version.VersionID]...)
	return hints
}

func extractPackageRegistryVersions(envelopes []facts.Envelope) []packageRegistryVersionIdentity {
	out := make([]packageRegistryVersionIdentity, 0)
	seen := make(map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistryPackageVersionFactKind || envelope.IsTombstone {
			continue
		}
		version := packageRegistryVersionIdentity{
			FactID:      envelope.FactID,
			PackageID:   payloadStr(envelope.Payload, "package_id"),
			VersionID:   payloadStr(envelope.Payload, "version_id"),
			Version:     payloadStr(envelope.Payload, "version"),
			PublishedAt: payloadStr(envelope.Payload, "published_at"),
		}
		if version.PackageID == "" || version.VersionID == "" || version.Version == "" {
			continue
		}
		if _, ok := seen[version.VersionID]; ok {
			continue
		}
		seen[version.VersionID] = struct{}{}
		out = append(out, version)
	}
	return out
}
