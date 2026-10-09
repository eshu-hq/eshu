// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// appSetRelation names how the repository an ApplicationSet template deploys
// (D) relates to the repository its generator reads config from (G) and the
// repository that holds the ApplicationSet (the control repository, C).
type appSetRelation struct {
	name string
	// generatorAlias and configScope say where the generator reads config.
	generatorAlias string
	configScope    string
	// deployedAlias is the template source the config names.
	deployedAlias string
	// deployedRepoID is the catalog repository the template source resolves
	// to, or "" when it must not yield a deployment fact (it is the control
	// repository).
	deployedRepoID string
	// knownSilentSkip marks a relation the extractor drops today even though
	// its template source resolves to a non-control repository. The test pins
	// the drop so a fix has to update it deliberately; the open debt is
	// tracked in #7778 (generator reads config from the control repository).
	knownSilentSkip bool
}

var appSetRelations = []appSetRelation{
	{name: "D==G", generatorAlias: "app-repo", configScope: "repo-app", deployedAlias: "app-repo", deployedRepoID: "repo-app"},
	{name: "D!=G", generatorAlias: "central-config", configScope: "repo-central", deployedAlias: "other-service", deployedRepoID: "repo-other"},
	{name: "D==C", generatorAlias: "central-config", configScope: "repo-central", deployedAlias: "gitops-repo", deployedRepoID: ""},
	{name: "G==C", generatorAlias: "gitops-repo", configScope: "repo-gitops", deployedAlias: "other-service", deployedRepoID: "repo-other", knownSilentSkip: true},
}

// appSetInvariantEnvelopes builds the YAML or the structured form of one
// relation and destination.
func appSetInvariantEnvelopes(relation appSetRelation, structured, literal bool) []facts.Envelope {
	destination, server := appSetTemplatedDestination, "{{ .server }}"
	if literal {
		destination, server = appSetLiteralDestination, "https://kubernetes.default.svc"
	}
	if structured {
		return []facts.Envelope{appSetStructuredEnvelope(relation.generatorAlias, relation.deployedAlias, server)}
	}
	return []facts.Envelope{
		appSetConfigRepoSet(relation.generatorAlias, destination),
		appSetConfigRepoConfig(relation.configScope, "prod", relation.deployedAlias),
	}
}

// TestApplicationSetRelationInvariants runs every relation of the deployed
// repository to the generator and control repositories, with a literal and a
// templated destination, through the YAML and the structured path. It pins two
// invariants: no fact runs from a repository to itself, and every template
// source that resolves to a catalog repository other than the control
// repository yields at least one DEPLOYS_FROM fact involving it, so a future
// silent skip fails here.
func TestApplicationSetRelationInvariants(t *testing.T) {
	t.Parallel()

	for _, relation := range appSetRelations {
		for _, structured := range []bool{false, true} {
			for _, literal := range []bool{true, false} {
				name := relation.name + "/yaml"
				if structured {
					name = relation.name + "/structured"
				}
				if literal {
					name += "/literal"
				} else {
					name += "/templated"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					evidence := DiscoverEvidence(appSetInvariantEnvelopes(relation, structured, literal), appSetConfigRepoCatalog)

					for _, fact := range evidence {
						if fact.SourceRepoID != "" && fact.SourceRepoID == fact.TargetRepoID {
							t.Fatalf("self-loop %s %s -> %s", fact.EvidenceKind, fact.SourceRepoID, fact.TargetRepoID)
						}
					}

					involving := 0
					for _, fact := range evidence {
						if fact.RelationshipType == RelDeploysFrom && relation.deployedRepoID != "" &&
							(fact.SourceRepoID == relation.deployedRepoID || fact.TargetRepoID == relation.deployedRepoID) {
							involving++
						}
					}
					switch {
					case relation.deployedRepoID == "":
						for _, kind := range []EvidenceKind{
							EvidenceKindArgoCDApplicationSetDeploySource,
							EvidenceKindArgoCDApplicationSetTemplateSource,
						} {
							if got := evidenceOfKind(evidence, kind); len(got) != 0 {
								t.Fatalf("control repo as template source produced %s: %#v", kind, got)
							}
						}
					case relation.knownSilentSkip:
						if involving != 0 {
							t.Fatalf("known silent skip %s now yields %d facts; update the table", relation.name, involving)
						}
					default:
						if involving == 0 {
							t.Fatalf("template source %s resolves to %s but no DEPLOYS_FROM fact involves it: %#v",
								relation.deployedAlias, relation.deployedRepoID, evidence)
						}
					}

					wantPlatform := 0
					if literal && relation.deployedRepoID != "" && !relation.knownSilentSkip {
						wantPlatform = 1
					}
					if got := len(evidenceOfKind(evidence, EvidenceKindArgoCDDestinationPlatform)); got != wantPlatform {
						t.Fatalf("platform facts = %d, want %d", got, wantPlatform)
					}
				})
			}
		}
	}
}
