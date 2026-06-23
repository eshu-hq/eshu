package reducer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeImportEvidenceSource labels repo-to-repo DEPENDS_ON edges derived from
// per-file external import sources correlated to package-registry ownership. It
// is deliberately distinct from packageConsumptionEvidenceSource
// ("projection/package-consumption") and crossRepoEvidenceSource
// ("resolver/cross-repo") so the code-import edges are independently retractable
// and so query surfaces can attribute the edge to parser import provenance
// (issue #3642).
const codeImportEvidenceSource = "projection/code-imports"

// codeImportRepoEdgeConfidence is the fixed confidence for code-import-derived
// edges. It mirrors packageConsumptionRepoEdgeConfidence: the owning repository
// is resolved from a package source hint (exact/derived URL match) rather than
// an explicit cross-repo runtime reference, so it sits below resolver/cross-repo
// and runtime service-list confidence.
const codeImportRepoEdgeConfidence = 0.6

// CodeImportRepoDependencyInput carries the per-file import facts for one
// reducer scope plus the already-resolved package-owner index and the acceptance
// identity used to enqueue durable repo-dependency projection intents.
//
// Owners is the codeImportOwnerIndex built from exact/derived
// PackagePublicationDecision and PackageSourceCorrelationDecision records joined
// to package-registry identity facts on the sanctioned (ecosystem, name) key
// (the same join issue #3598 uses) via buildCodeImportOwnerIndex.
type CodeImportRepoDependencyInput struct {
	ScopeID       string
	GenerationID  string
	SourceRunID   string
	CreatedAt     time.Time
	FileEnvelopes []facts.Envelope
	Owners        codeImportOwnerIndex
}

// BuildCodeImportRepoDependencyIntents scans per-file import sources, normalizes
// each external import to an (ecosystem, package coordinate), resolves the
// coordinate to an owning repository through the package-registry decision
// space, and returns deduplicated consumer-repo DEPENDS_ON owner-repo upsert
// intents for the shared repo-dependency projection lane.
//
// Conservatism is the top correctness rule (eshu-correlation-truth): only
// exact/derived owner resolutions reach Owners, and any import source that is
// relative, intra-repo, unresolved, or owned by no indexed repository is
// dropped, never guessed. Self-references (consumer == owner) are dropped.
// Multiple files importing packages that resolve to the same consumer/owner pair
// collapse to one edge whose evidence_count records how many distinct packages
// backed it.
//
// The returned rows reuse BuildSharedProjectionIntent so the intent id is a
// deterministic function of the acceptance identity and partition key; running
// the join twice over the same input yields identical intent ids, which keeps
// the downstream DEPENDS_ON MERGE idempotent under retries and re-projection.
// Overlap with the #3598 manifest-consumption edges is intentional and safe: the
// MERGE keyed on (source_repo, target_repo) is idempotent, and net-new
// code-import edges add value where no manifest declares the dependency.
func BuildCodeImportRepoDependencyIntents(
	input CodeImportRepoDependencyInput,
) []SharedProjectionIntentRow {
	if input.Owners.empty() || len(input.FileEnvelopes) == 0 {
		return nil
	}

	type edgeAccumulator struct {
		consumerRepoID string
		ownerRepoID    string
		packageCoords  map[string]struct{}
		ecosystems     map[string]struct{}
	}

	edges := make(map[string]*edgeAccumulator)
	order := make([]string, 0)

	for _, envelope := range input.FileEnvelopes {
		if envelope.FactKind != factKindFile {
			continue
		}
		consumerRepoID := strings.TrimSpace(payloadStr(envelope.Payload, "repo_id"))
		if consumerRepoID == "" {
			continue
		}
		language := strings.TrimSpace(payloadStr(envelope.Payload, "language"))
		fileData, ok := envelope.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}

		for _, entry := range mapSlice(fileData["imports"]) {
			source := codeImportEntrySource(entry)
			if source == "" {
				continue
			}
			ecosystem, coordinate := normalizeImportSource(source, language)
			if ecosystem == "" || coordinate == "" {
				continue
			}
			ownerRepoID := matchImportCoordinateToOwner(ecosystem, coordinate, input.Owners)
			if ownerRepoID == "" {
				continue
			}
			if ownerRepoID == consumerRepoID {
				continue
			}

			edgeKey := consumerRepoID + "\x00" + ownerRepoID
			acc, exists := edges[edgeKey]
			if !exists {
				acc = &edgeAccumulator{
					consumerRepoID: consumerRepoID,
					ownerRepoID:    ownerRepoID,
					packageCoords:  make(map[string]struct{}),
					ecosystems:     make(map[string]struct{}),
				}
				edges[edgeKey] = acc
				order = append(order, edgeKey)
			}
			acc.packageCoords[ecosystem+":"+coordinate] = struct{}{}
			acc.ecosystems[ecosystem] = struct{}{}
		}
	}

	if len(edges) == 0 {
		return nil
	}

	sort.Strings(order)
	rows := make([]SharedProjectionIntentRow, 0, len(order))
	for _, edgeKey := range order {
		acc := edges[edgeKey]
		rows = append(rows, buildCodeImportRepoEdgeIntent(
			input,
			acc.consumerRepoID,
			acc.ownerRepoID,
			len(acc.packageCoords),
			sortedKeys(acc.ecosystems),
		))
	}
	return rows
}

// codeImportEntrySource returns the external import identity for one parser
// import entry, preferring the language-resolved source over the raw source.
// resolved_source is preferred because it carries the parser's resolution of
// alias/baseUrl rewrites; for external packages the two usually agree, but when
// resolved_source resolves a baseUrl import to an intra-repo path the normalizer
// will drop it as a relative/intra-repo specifier, which is the conservative
// outcome.
func codeImportEntrySource(entry map[string]any) string {
	if resolved := strings.TrimSpace(anyToString(entry["resolved_source"])); resolved != "" {
		return resolved
	}
	return strings.TrimSpace(anyToString(entry["source"]))
}

// buildCodeImportRepoEdgeIntent builds one upsert intent for a single
// consumer-repo DEPENDS_ON owner-repo edge backed by code-import evidence.
func buildCodeImportRepoEdgeIntent(
	input CodeImportRepoDependencyInput,
	consumerRepoID string,
	ownerRepoID string,
	packageCount int,
	ecosystems []string,
) SharedProjectionIntentRow {
	partitionKey := fmt.Sprintf("repo:%s->%s", consumerRepoID, ownerRepoID)
	rationale := fmt.Sprintf(
		"Consumer repository imports %d external package(s) owned by target repository",
		packageCount,
	)
	payload := map[string]any{
		"action":            "upsert",
		"repo_id":           consumerRepoID,
		"target_repo_id":    ownerRepoID,
		"relationship_type": "DEPENDS_ON",
		"evidence_source":   codeImportEvidenceSource,
		"evidence_type":     "code_imports",
		"resolution_source": codeImportEvidenceSource,
		"confidence":        codeImportRepoEdgeConfidence,
		"evidence_count":    packageCount,
		"evidence_kinds":    ecosystems,
		"generation_id":     input.GenerationID,
		"rationale":         rationale,
		"resolved_id":       codeImportResolvedID(consumerRepoID, ownerRepoID),
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     partitionKey,
		ScopeID:          input.ScopeID,
		AcceptanceUnitID: consumerRepoID,
		RepositoryID:     consumerRepoID,
		SourceRunID:      strings.TrimSpace(input.SourceRunID),
		GenerationID:     input.GenerationID,
		Payload:          payload,
		CreatedAt:        input.CreatedAt,
	})
}

// codeImportResolvedID is the stable provenance id for one code-import-derived
// edge, distinct from package-consumption and resolver/cross-repo resolved ids.
func codeImportResolvedID(consumerRepoID, ownerRepoID string) string {
	return "code-imports:" + consumerRepoID + "->" + ownerRepoID
}

// codeImportOwnerIndex resolves an import (ecosystem, coordinate) to its owning
// repository through the sanctioned (ecosystem, name) consumption key. It is the
// composition of two maps proven correct by issue #3598:
//
//   - byKey: packageConsumptionKeys(ecosystem, name) -> owning RepositoryID,
//     built from package-registry identity facts joined to exact/derived
//     ownership/publication decisions on PackageID.
//   - ambiguous: consumption keys that resolved to more than one distinct owning
//     repository. A coordinate that maps to an ambiguous key is dropped, never
//     guessed (eshu-correlation-truth).
type codeImportOwnerIndex struct {
	byKey     map[string]string
	ambiguous map[string]struct{}
}

// empty reports whether the index resolves no owners.
func (idx codeImportOwnerIndex) empty() bool {
	return len(idx.byKey) == 0
}

// lookup returns the owning repository for one (ecosystem, coordinate). It
// returns "" when no key matches or when every matching key is ambiguous.
func (idx codeImportOwnerIndex) lookup(ecosystem, coordinate string) string {
	for _, key := range packageConsumptionKeys(ecosystem, coordinate) {
		if _, bad := idx.ambiguous[key]; bad {
			continue
		}
		if repoID := idx.byKey[key]; repoID != "" {
			return repoID
		}
	}
	return ""
}

// buildCodeImportOwnerIndex builds the (ecosystem, name) -> owning-repository
// index from package-registry identity facts and the exact/derived ownership and
// publication decisions. It reuses extractPackageRegistryIdentities and
// resolvePackageOwners so the join key and owner admission match issue #3598
// exactly; no new ownership invariant is asserted.
//
// A consumption key that resolves to more than one distinct owning repository is
// recorded as ambiguous and excluded from byKey, so a later lookup returns no
// owner rather than picking one arbitrarily.
func buildCodeImportOwnerIndex(
	envelopes []facts.Envelope,
	ownership []PackageSourceCorrelationDecision,
	publication []PackagePublicationDecision,
) codeImportOwnerIndex {
	ownersByPackageID := resolvePackageOwners(ownership, publication)
	if len(ownersByPackageID) == 0 {
		return codeImportOwnerIndex{}
	}

	byKey := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, identity := range extractPackageRegistryIdentities(envelopes) {
		owner, ok := ownersByPackageID[strings.TrimSpace(identity.PackageID)]
		if !ok || owner.repoID == "" {
			continue
		}
		for _, name := range identity.Names {
			for _, key := range packageConsumptionKeys(identity.Ecosystem, name) {
				if _, bad := ambiguous[key]; bad {
					continue
				}
				existing, seen := byKey[key]
				if seen && existing != owner.repoID {
					// Same coordinate, two different owners: ambiguous, drop it.
					delete(byKey, key)
					ambiguous[key] = struct{}{}
					continue
				}
				byKey[key] = owner.repoID
			}
		}
	}
	return codeImportOwnerIndex{byKey: byKey, ambiguous: ambiguous}
}
