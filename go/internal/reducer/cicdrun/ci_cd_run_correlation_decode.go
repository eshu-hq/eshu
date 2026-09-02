// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// trimmedCICDField trims a required (non-pointer) ci_cd_run identity/anchor
// field to preserve byte-parity with the pre-migration read path. The old
// reducer read every ci.* payload key through payloadcore.PayloadString, which did
// strings.TrimSpace(fmt.Sprint(value)) on every read
// (go/internal/reducer/package_correlation_writer.go). The typed decode seam
// preserves the raw collector string, so the correlation key, the anchor
// emptiness checks, and the `== "workflow_image_ref"`/`== "shell"` compares
// must trim at the point of use to stay identical: a padded run_id must join
// under the clean key and a whitespace-only commit_sha must count as empty
// (unresolved), exactly as the trimmed path did. The typed struct still
// carries the raw collector value; only the correlation logic trims.
func trimmedCICDField(value string) string {
	return strings.TrimSpace(value)
}

// TrimmedCICDPtr trims an optional (pointer) ci_cd_run field, treating a nil
// pointer as the empty string. It is trimmedCICDField composed with
// payloadcore.DerefString, for the many optional identity/anchor fields the correlation
// logic consumes (run_attempt, repository_id, commit_sha, environment,
// artifact_digest, evidence_class, image_ref, deployment_hint_source).
func TrimmedCICDPtr(value *string) string {
	return strings.TrimSpace(payloadcore.DerefString(value))
}

// buildCICDRunCorrelationDecisionsWithQuarantine is the quarantine-aware core
// BuildCICDRunCorrelationDecisions and CICDRunCorrelationHandler.Handle both
// use. It decodes every ci.run/ci.artifact/ci.environment_observation/
// ci.trigger_edge/ci.step/ci.workflow_image_evidence envelope through the
// sdk/go/factschema typed seam: a fact missing its required join-key field
// (provider/run_id for the five run-scoped kinds, repository_id for workflow
// image evidence) is routed through factdecode.PartitionDecodeFailures so it dead-letters
// as a per-fact input_invalid quarantine instead of silently joining under an
// empty-string key, while every valid fact in the same batch still projects.
// A non-quarantinable decode error (an unsupported schema major) is returned
// fatally so the whole intent fails for durable triage.
// The fourth return is the number of ci.deployment_event facts the attach
// dropped because the event and its sha-matching run named different
// repositories. It is surfaced rather than swallowed because that drop is
// total for the affected run and otherwise invisible: the collector's
// deployment_unanchored warning keys on sha, not repository, so it cannot fire
// for this condition, and validateTarget can only reject a PATH disagreement at
// startup because the run's repository html_url is unknown until collection.
func buildCICDRunCorrelationDecisionsWithQuarantine(envelopes []facts.Envelope) ([]CICDRunCorrelationDecision, []factdecode.QuarantinedFact, int, error) {
	runs := map[string]*cicdRunEvidence{}
	var workflowImages []*decodedCICDWorkflowImage
	var deploymentEvents []*decodedCICDDeploymentEvent
	var quarantined []factdecode.QuarantinedFact
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.CICDRunFactKind:
			run, err := schemadecode.DecodeCICDRun(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			ev := ensureCICDRunEvidence(runs, CICDRunKeyFromParts(run.Provider, run.RunID, run.RunAttempt))
			ev.run = envelope
			ev.runDecoded = run
		case facts.CICDArtifactFactKind:
			artifact, err := schemadecode.DecodeCICDArtifact(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			ev := ensureCICDRunEvidence(runs, CICDRunKeyFromParts(artifact.Provider, artifact.RunID, artifact.RunAttempt))
			ev.artifacts = append(ev.artifacts, envelope)
			ev.artifactsDecoded = append(ev.artifactsDecoded, artifact)
		case facts.CICDEnvironmentObservationFactKind:
			observation, err := schemadecode.DecodeCICDEnvironmentObservation(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			ev := ensureCICDRunEvidence(runs, CICDRunKeyFromParts(observation.Provider, observation.RunID, observation.RunAttempt))
			ev.environments = append(ev.environments, envelope)
			ev.environmentsDecoded = append(ev.environmentsDecoded, observation)
		case facts.CICDDeploymentEventFactKind:
			// A deployment event carries no run_id at all -- GitHub's
			// Deployments API has no run identity -- so it cannot be bucketed
			// under a run here the way the run-scoped kinds above are.
			// Decode it once and collect it into the flat deploymentEvents
			// slice; attachDeploymentEventsToRuns fans it out to every run
			// whose commit sha matches after this loop, matching the
			// ci.workflow_image_evidence pattern (repo-scoped evidence
			// attached run-side, not decode-side).
			event, err := schemadecode.DecodeCICDDeploymentEvent(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			deploymentEvents = append(deploymentEvents, &decodedCICDDeploymentEvent{
				envelope: envelope,
				evidence: event,
			})
		case facts.CICDTriggerEdgeFactKind:
			edge, err := schemadecode.DecodeCICDTriggerEdge(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			ev := ensureCICDRunEvidence(runs, CICDRunKeyFromParts(edge.Provider, edge.RunID, edge.RunAttempt))
			ev.triggers = append(ev.triggers, envelope)
		case facts.CICDStepFactKind:
			step, err := schemadecode.DecodeCICDStep(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			if TrimmedCICDPtr(step.DeploymentHintSource) == "shell" {
				ev := ensureCICDRunEvidence(runs, CICDRunKeyFromParts(step.Provider, step.RunID, step.RunAttempt))
				ev.shellOnly = append(ev.shellOnly, envelope)
			}
		case facts.CICDWorkflowImageEvidenceFactKind:
			// Decode the workflow-image evidence exactly ONCE here (this is
			// also the quarantine-check decode) and carry the typed value on
			// decodedCICDWorkflowImage, so attachWorkflowImagesToRuns and
			// classifyCICDWorkflowImageEvidence read the cached struct instead
			// of re-decoding the same envelope once per run in the repo (the
			// O(runs x workflow_images) re-decode the copilot #4724 review
			// flagged).
			evidence, err := schemadecode.DecodeCICDWorkflowImageEvidence(envelope)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(envelope, err)
				if !ok {
					return nil, nil, 0, fatal
				}
				quarantined = append(quarantined, q)
				continue
			}
			workflowImages = append(workflowImages, &decodedCICDWorkflowImage{
				envelope: envelope,
				evidence: evidence,
			})
		}
	}
	attachWorkflowImagesToRuns(runs, workflowImages)
	deploymentEventsSkipped := attachDeploymentEventsToRuns(runs, deploymentEvents)
	imageIndex := buildCICDImageIdentityIndex(envelopes)
	decisions := make([]CICDRunCorrelationDecision, 0, len(runs))
	for _, ev := range runs {
		if ev.run.FactID == "" {
			continue
		}
		decisions = append(decisions, classifyCICDRunEvidence(ev, imageIndex))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].Provider+decisions[i].RunID < decisions[j].Provider+decisions[j].RunID
	})
	return decisions, quarantined, deploymentEventsSkipped, nil
}

// ciArtifactDigests collects the distinct artifact_digest values across every
// ci.artifact envelope, decoded through the typed seam, to bound the active
// container-image-identity lookup ListActiveCICDRunCorrelationFacts issues
// before the main decode/classify pass. A fact that fails to decode here is
// silently skipped (contributes no digest to the bounding query) rather than
// quarantined: the main buildCICDRunCorrelationDecisionsWithQuarantine pass
// decodes the same envelope again and is the single place that records the
// visible input_invalid quarantine, so this pre-pass would otherwise
// double-count the same malformed fact.
func ciArtifactDigests(envelopes []facts.Envelope) []string {
	var digests []string
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDArtifactFactKind {
			continue
		}
		artifact, err := schemadecode.DecodeCICDArtifact(envelope)
		if err != nil {
			continue
		}
		if digest := TrimmedCICDPtr(artifact.ArtifactDigest); digest != "" {
			digests = append(digests, digest)
		}
	}
	return payloadcore.UniqueSortedStrings(digests)
}

// ciWorkflowImageRefs collects the distinct image_ref values across every
// resolvable (evidence_class=="workflow_image_ref") ci.workflow_image_evidence
// envelope, decoded through the typed seam, to bound the same active
// container-image-identity lookup. See ciArtifactDigests for why a decode
// failure here is silently skipped rather than quarantined.
func ciWorkflowImageRefs(envelopes []facts.Envelope) []string {
	var refs []string
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDWorkflowImageEvidenceFactKind {
			continue
		}
		evidence, err := schemadecode.DecodeCICDWorkflowImageEvidence(envelope)
		if err != nil {
			continue
		}
		if TrimmedCICDPtr(evidence.EvidenceClass) != "workflow_image_ref" {
			continue
		}
		if ref := TrimmedCICDPtr(evidence.ImageRef); ref != "" {
			refs = append(refs, ref)
		}
	}
	return payloadcore.UniqueSortedStrings(refs)
}

// decodedCICDWorkflowImage pairs a ci.workflow_image_evidence envelope with
// its once-decoded typed value. attachWorkflowImagesToRuns fans the same
// evidence out to every run in a repo, and classifyCICDWorkflowImageEvidence
// then reads it per run; caching the decode here (performed once during the
// build phase) keeps a repo's workflow-image evidence from re-decoding
// O(runs x workflow_images) times (copilot #4724 review).
type decodedCICDWorkflowImage struct {
	envelope facts.Envelope
	evidence cicdrunv1.WorkflowImageEvidence
}

// cicdRunEvidence accumulates every decoded fact joined to one provider run
// (keyed by CICDRunKeyFromParts), alongside the original facts.Envelope for
// each so classifyCICDRunEvidence can still report FactIDs on the decision.
type cicdRunEvidence struct {
	run                 facts.Envelope
	runDecoded          cicdrunv1.Run
	artifacts           []facts.Envelope
	artifactsDecoded    []cicdrunv1.Artifact
	environments        []facts.Envelope
	environmentsDecoded []cicdrunv1.EnvironmentObservation
	triggers            []facts.Envelope
	shellOnly           []facts.Envelope
	workflowImages      []*decodedCICDWorkflowImage
	// deploymentEvents holds every ci.deployment_event whose sha matched this
	// run's CommitSHA (attachDeploymentEventsToRuns), repo-scoped evidence
	// attached run-side because the fact carries no run_id to bucket it under
	// during decode, matching how workflowImages is accumulated.
	deploymentEvents []*decodedCICDDeploymentEvent
	// workflowImagesCommitMatched is true when workflowImages were attached
	// because their extraction commit matched this run's commit, and false
	// when they are the commit-blind repository-wide fallback. It downgrades a
	// fallback workflow-image correlation from exact to derived (#5424).
	workflowImagesCommitMatched bool
}

func ensureCICDRunEvidence(runs map[string]*cicdRunEvidence, key string) *cicdRunEvidence {
	if runs[key] == nil {
		runs[key] = &cicdRunEvidence{}
	}
	return runs[key]
}

// cicdImageIdentity is one reducer-owned container-image identity row read
// from the (out-of-scope, raw-payload) reducercontract.ContainerImageIdentityFactKind facts
// buildCICDImageIdentityIndex indexes by digest.
type cicdImageIdentity struct {
	factID string
	// evidenceFactIDs preserves every support-grain fact that agreed on this
	// digest-qualified image identity. The support store intentionally returns
	// one envelope per evidence source; CI/CD correlation must count distinct
	// image identities, not those support rows.
	evidenceFactIDs []string
	// buildProvenanceRepositoryIDs carries only the repositories the identity
	// decision attributed BUILD evidence to, the narrow set #5796 established
	// for this exact distinction. It is the sole join key for repository
	// narrowing (#5823). A row published before that key existed carries an
	// empty set and is simply never selected, which reproduces the pre-#5766
	// behavior for those rows rather than degrading it -- see
	// cicdImageMatchesForRepository.
	buildProvenanceRepositoryIDs []string
	imageRef                     string
	digest                       string
}

func buildCICDImageIdentityIndex(envelopes []facts.Envelope) map[string][]cicdImageIdentity {
	index := map[string][]cicdImageIdentity{}
	positions := map[string]map[string]int{}
	for _, envelope := range envelopes {
		if envelope.FactKind != reducercontract.ContainerImageIdentityFactKind {
			continue
		}
		digest := payloadcore.PayloadString(envelope.Payload, "digest")
		if digest == "" {
			continue
		}
		identity := cicdImageIdentity{
			factID:          envelope.FactID,
			evidenceFactIDs: []string{envelope.FactID},
			buildProvenanceRepositoryIDs: payloadcore.PayloadOrderedStrings(
				envelope.Payload, "build_provenance_repository_ids",
			),
			imageRef: payloadcore.PayloadString(envelope.Payload, "image_ref"),
			digest:   digest,
		}
		if identity.imageRef == "" {
			index[digest] = append(index[digest], identity)
			continue
		}
		if positions[digest] == nil {
			positions[digest] = map[string]int{}
		}
		if position, ok := positions[digest][identity.imageRef]; ok {
			existing := &index[digest][position]
			existing.evidenceFactIDs = payloadcore.UniqueSortedStrings(append(
				existing.evidenceFactIDs, envelope.FactID,
			))
			existing.buildProvenanceRepositoryIDs = payloadcore.UniqueSortedStrings(append(
				existing.buildProvenanceRepositoryIDs,
				identity.buildProvenanceRepositoryIDs...,
			))
			continue
		}
		positions[digest][identity.imageRef] = len(index[digest])
		index[digest] = append(index[digest], identity)
	}
	return index
}

func cicdImageIdentityEvidenceFactIDs(identity cicdImageIdentity) []string {
	if len(identity.evidenceFactIDs) > 0 {
		return identity.evidenceFactIDs
	}
	if identity.factID == "" {
		return nil
	}
	return []string{identity.factID}
}

// CICDRunKeyFromParts builds the reducer's run join key from typed decoded
// identity fields (provider, run_id, run_attempt), reading from a decoded
// cicdrunv1 struct's already-validated Provider/RunID rather than a raw
// payload map. It is now the ONLY run-key builder in the package: the raw
// payload-map equivalent this function replaced (cicdRunKey) was deleted once
// container_image_identity_evidence.go's own ci.run/ci.artifact reads
// migrated to this typed seam (#4685) and no caller remained. Every segment
// is trimmed (trimmedCICDField / defaultCICDRunAttempt, which also trims) so
// the key is byte-identical to the pre-migration raw-payload read, which read
// each segment through the whitespace-trimming payloadcore.PayloadString: a padded run_id
// (" run-1 ") must join under the clean "run-1" key rather than splitting a
// run's evidence across a padded and a clean bucket.
func CICDRunKeyFromParts(provider, runID string, runAttempt *string) string {
	return trimmedCICDField(provider) + ":" + trimmedCICDField(runID) + ":" + defaultCICDRunAttempt(payloadcore.DerefString(runAttempt))
}
