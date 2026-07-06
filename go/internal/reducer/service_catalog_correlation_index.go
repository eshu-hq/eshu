// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const serviceCatalogGitRepositoryScopePrefix = "git-repository-scope:"

type serviceCatalogEntityEvidence struct {
	factID             string
	provider           string
	entityRef          string
	entityType         string
	displayName        string
	lifecycle          string
	tier               string
	sourceRepositoryID string
	serviceID          string
	workloadID         string
}

type serviceCatalogEntityKey struct {
	provider  string
	entityRef string
}

type serviceCatalogOwnershipEvidence struct {
	factID    string
	provider  string
	entityRef string
	ownerRef  string
}

type serviceCatalogRepositoryLinkEvidence struct {
	factID         string
	provider       string
	entityRef      string
	repositoryID   string
	repositoryURL  string
	repositoryName string
	serviceID      string
	workloadID     string
}

type serviceCatalogRepositoryEvidence struct {
	factID       string
	repositoryID string
	name         string
	remoteURL    string
	tombstone    bool
}

// buildServiceCatalogCorrelationIndex builds the correlation index, silently
// discarding any fact that fails to decode. It exists for the public
// BuildServiceCatalogCorrelationDecisions entrypoint and pre-migration test
// callers whose signature cannot observe quarantined or fatal facts; Handle
// calls buildServiceCatalogCorrelationIndexWithQuarantine directly so a
// malformed fact is recorded as a visible dead-letter, not silently dropped,
// and a fatal decode error fails the whole intent.
func buildServiceCatalogCorrelationIndex(envelopes []facts.Envelope) serviceCatalogCorrelationIndex {
	index, _, _ := buildServiceCatalogCorrelationIndexWithQuarantine(envelopes)
	return index
}

// buildServiceCatalogCorrelationIndexWithQuarantine decodes each
// service_catalog.entity, service_catalog.ownership, and
// service_catalog.repository_link envelope's outer identity through the
// contracts seam (decodeServiceCatalogEntity, decodeServiceCatalogOwnership,
// decodeServiceCatalogRepositoryLink) and the codegraph "repository" envelope
// through decodeCodegraphRepository (reused from Wave 4f S1), returning
// (index, quarantined, fatalErr).
//
// A fact whose payload is missing its required entity_ref (or, for a codegraph
// repository, repo_id) identity field is a QUARANTINABLE input_invalid: it is
// recorded as a quarantinedFact and EXCLUDED from the index — exactly the set
// the pre-migration payloadString reads silently dropped via their
// blank-string guards, but now with a visible, operator-diagnosable
// dead-letter (Contract System v1 Wave 4f S3, issue #4755).
//
// A FATAL decode error — a payload type mismatch, or an unsupported schema
// major (service_catalog is registered and schema-version-admitted, so unlike
// the unregistered codegraph file/repository kinds an unsupported major IS a
// reachable class here) — is NOT quarantined: partitionDecodeFailures returns
// it as the fatal third result and this function returns it as fatalErr so the
// handler fails the whole work item through WorkSink.Fail (which triages it for
// retry once the reducer supports the new major), rather than publishing
// version-skewed service-catalog truth with the offending fact silently
// omitted. On a fatal error the partial index and quarantine slice are
// discarded by the caller.
func buildServiceCatalogCorrelationIndexWithQuarantine(
	envelopes []facts.Envelope,
) (serviceCatalogCorrelationIndex, []quarantinedFact, error) {
	index := serviceCatalogCorrelationIndex{
		entities:  make(map[serviceCatalogEntityKey]serviceCatalogEntityEvidence),
		ownership: make(map[serviceCatalogEntityKey]serviceCatalogOwnershipEvidence),
		repoLinks: make(map[serviceCatalogEntityKey][]serviceCatalogRepositoryLinkEvidence),
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.ServiceCatalogEntityFactKind:
			entity, err := serviceCatalogEntityFromFact(envelope)
			if err != nil {
				q, ok, fatal := serviceCatalogQuarantine(envelope, err)
				if !ok {
					return serviceCatalogCorrelationIndex{}, nil, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			if entity.entityRef == "" {
				// A present-but-empty entity_ref is a valid decoded fact (the
				// typed contract accepts present-but-empty required fields) but
				// carries no usable catalog identity, so it is NOT indexed —
				// exactly as the pre-migration `if entity.entityRef != ""`
				// guard did. Only an ABSENT entity_ref dead-letters (the decode
				// error above); a present-but-blank one is simply skipped, never
				// keyed under an empty-string identity.
				continue
			}
			index.entities[entity.key()] = entity
		case facts.ServiceCatalogOwnershipFactKind:
			owner, err := serviceCatalogOwnershipFromFact(envelope)
			if err != nil {
				q, ok, fatal := serviceCatalogQuarantine(envelope, err)
				if !ok {
					return serviceCatalogCorrelationIndex{}, nil, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			if owner.ownerRef == "" || owner.entityRef == "" {
				// A present-but-empty owner reference (both owner_ref and its
				// legacy owner fallback absent or blank) or a present-but-empty
				// entity_ref is a valid decoded fact carrying no usable
				// ownership claim, not a malformed payload — it is simply not
				// indexed, matching the pre-migration
				// `if owner.ownerRef != "" && owner.entityRef != ""` guard
				// (never quarantined).
				continue
			}
			index.ownership[owner.key()] = owner
		case facts.ServiceCatalogRepositoryLinkFactKind:
			link, err := serviceCatalogRepositoryLinkFromFact(envelope)
			if err != nil {
				q, ok, fatal := serviceCatalogQuarantine(envelope, err)
				if !ok {
					return serviceCatalogCorrelationIndex{}, nil, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			if link.entityRef == "" {
				// A present-but-empty entity_ref is a valid decoded fact but
				// carries no join identity, so it is NOT indexed — exactly as
				// the pre-migration `if link.entityRef != ""` guard did. Only an
				// ABSENT entity_ref dead-letters.
				continue
			}
			index.repoLinks[link.key()] = append(index.repoLinks[link.key()], link)
		case factKindRepository:
			repository, err := serviceCatalogRepositoryFromFact(envelope)
			if err != nil {
				q, ok, fatal := serviceCatalogQuarantine(envelope, err)
				if !ok {
					return serviceCatalogCorrelationIndex{}, nil, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			if repository.repositoryID == "" {
				// repositoryID is firstNonBlank(graph_id, repo_id); a repository
				// with both present-but-empty resolves to "" and carries no
				// canonical identity to correlate against, so it is NOT added —
				// exactly as the pre-migration `if repository.repositoryID != ""`
				// guard did. (An ABSENT repo_id dead-letters at decode above;
				// this drop is only for the present-but-both-empty case.)
				continue
			}
			index.repositories = append(index.repositories, repository)
		}
	}
	sort.SliceStable(index.repositories, func(i, j int) bool {
		return index.repositories[i].repositoryID < index.repositories[j].repositoryID
	})
	index.repositoryLookup = buildServiceCatalogRepositoryLookup(index.repositories)
	return index, quarantined, nil
}

// serviceCatalogQuarantine classifies a service_catalog (or reused codegraph
// repository) decode error through partitionDecodeFailures, returning
// (quarantinedFact, ok, fatalErr). When ok is true the fact is a quarantinable
// per-fact input_invalid (a missing/null required identity field) the caller
// records and skips. When ok is false the error is FATAL — a payload type
// mismatch or an unsupported schema major — and fatalErr is the underlying
// error the caller returns to fail the whole work item, never a per-fact
// quarantine. The residual quarantinedFact carries the decode error's own
// classification/field for the fatal case only so the caller can still surface
// it in a log if it chooses; ok is the authoritative signal.
//
// This tightens the earlier wrapper, which discarded partitionDecodeFailures's
// fatal third result and fell back to serviceCatalogDecodeQuarantine for it —
// swallowing an unsupported-major (version-skew) error into a per-fact
// quarantine and letting the handler publish incomplete service-catalog truth.
// Because service_catalog IS registered and schema-version-admitted, an
// unsupported major is a reachable class here (unlike the unregistered
// codegraph file/repository kinds), so the fatal result must propagate.
func serviceCatalogQuarantine(envelope facts.Envelope, err error) (quarantinedFact, bool, error) {
	if q, ok, _ := partitionDecodeFailures(envelope, err); ok {
		return q, true, nil
	}
	return serviceCatalogDecodeQuarantine(envelope, err), false, err
}

// serviceCatalogEntityFromFact decodes one service_catalog.entity envelope's
// outer identity through the contracts seam. A payload missing entity_ref
// returns a classified decode error; the caller quarantines it rather than
// indexing a blank-identity entity.
func serviceCatalogEntityFromFact(envelope facts.Envelope) (serviceCatalogEntityEvidence, error) {
	entity, err := decodeServiceCatalogEntity(envelope)
	if err != nil {
		return serviceCatalogEntityEvidence{}, err
	}
	return serviceCatalogEntityEvidence{
		factID:             envelope.FactID,
		provider:           stringPtrValue(entity.Provider),
		entityRef:          entity.EntityRef,
		entityType:         stringPtrValue(entity.EntityType),
		displayName:        stringPtrValue(entity.DisplayName),
		lifecycle:          stringPtrValue(entity.Lifecycle),
		tier:               stringPtrValue(entity.Tier),
		sourceRepositoryID: serviceCatalogSourceRepositoryID(envelope.ScopeID),
		serviceID:          stringPtrValue(entity.ServiceID),
		workloadID:         stringPtrValue(entity.WorkloadID),
	}, nil
}

func (entity serviceCatalogEntityEvidence) key() serviceCatalogEntityKey {
	return serviceCatalogEntityKey{
		provider:  entity.provider,
		entityRef: entity.entityRef,
	}
}

// serviceCatalogOwnershipFromFact decodes one service_catalog.ownership
// envelope's outer identity through the contracts seam. A payload missing
// entity_ref returns a classified decode error; the caller quarantines it.
// The owner reference itself may arrive under either owner_ref (preferred) or
// the legacy owner key — matched by firstNonBlank, exactly as before this
// conversion — and staying blank on both is a valid decoded fact carrying no
// ownership claim, not a decode failure.
func serviceCatalogOwnershipFromFact(envelope facts.Envelope) (serviceCatalogOwnershipEvidence, error) {
	ownership, err := decodeServiceCatalogOwnership(envelope)
	if err != nil {
		return serviceCatalogOwnershipEvidence{}, err
	}
	return serviceCatalogOwnershipEvidence{
		factID:    envelope.FactID,
		provider:  stringPtrValue(ownership.Provider),
		entityRef: ownership.EntityRef,
		ownerRef:  firstNonBlank(stringPtrValue(ownership.OwnerRef), stringPtrValue(ownership.OwnerLegacy)),
	}, nil
}

func (owner serviceCatalogOwnershipEvidence) key() serviceCatalogEntityKey {
	return serviceCatalogEntityKey{
		provider:  owner.provider,
		entityRef: owner.entityRef,
	}
}

// serviceCatalogRepositoryLinkFromFact decodes one
// service_catalog.repository_link envelope's outer identity through the
// contracts seam. A payload missing entity_ref returns a classified decode
// error; the caller quarantines it. Every repository-identifying field stays
// optional by design (servicecatalogv1.RepositoryLink's doc comment): a link
// carrying none of them still decodes, and the reducer's own correlation
// logic — not this decode step — classifies that as
// ServiceCatalogCorrelationRejected.
func serviceCatalogRepositoryLinkFromFact(envelope facts.Envelope) (serviceCatalogRepositoryLinkEvidence, error) {
	link, err := decodeServiceCatalogRepositoryLink(envelope)
	if err != nil {
		return serviceCatalogRepositoryLinkEvidence{}, err
	}
	return serviceCatalogRepositoryLinkEvidence{
		factID:    envelope.FactID,
		provider:  stringPtrValue(link.Provider),
		entityRef: link.EntityRef,
		repositoryID: firstNonBlank(
			stringPtrValue(link.RepositoryID),
			stringPtrValue(link.RepoID),
		),
		repositoryURL: firstNonBlank(
			stringPtrValue(link.NormalizedURL),
			stringPtrValue(link.RepositoryURL),
			stringPtrValue(link.RawURL),
			stringPtrValue(link.URL),
		),
		repositoryName: stringPtrValue(link.RepositoryName),
		serviceID:      stringPtrValue(link.ServiceID),
		workloadID:     stringPtrValue(link.WorkloadID),
	}, nil
}

func (link serviceCatalogRepositoryLinkEvidence) key() serviceCatalogEntityKey {
	return serviceCatalogEntityKey{
		provider:  link.provider,
		entityRef: link.entityRef,
	}
}

// serviceCatalogRepositoryFromFact decodes one codegraph "repository"
// envelope's outer identity through decodeCodegraphRepository, reused
// unchanged from Wave 4f S1 (factschema_decode_codegraph.go). A payload
// missing repo_id returns a classified decode error; the caller quarantines
// it via codegraphDecodeQuarantine/partitionDecodeFailures, exactly like the
// code-graph-core reducer's own "repository" reads.
func serviceCatalogRepositoryFromFact(envelope facts.Envelope) (serviceCatalogRepositoryEvidence, error) {
	repository, err := decodeCodegraphRepository(envelope)
	if err != nil {
		return serviceCatalogRepositoryEvidence{}, err
	}
	return serviceCatalogRepositoryEvidence{
		factID:       envelope.FactID,
		repositoryID: firstNonBlank(stringPtrValue(repository.GraphID), repository.RepoID),
		name:         stringPtrValue(repository.Name),
		remoteURL:    stringPtrValue(repository.RemoteURL),
		tombstone:    envelope.IsTombstone,
	}, nil
}

// stringPtrValue dereferences an optional *string field to its value,
// returning "" for a nil pointer (an absent payload key) — the same
// zero-value the pre-migration payloadString read produced for a missing
// key, so every optional-field read in this file stays byte-identical to its
// pre-conversion behavior.
func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// classifyServiceCatalogEntity, the repository-matching helpers, and the
// per-decision shaping functions live in service_catalog_correlation_classify.go
// (split out to keep this file under the 500-line cap; this file owns fact
// decode + index construction, the classify file owns decision logic).
