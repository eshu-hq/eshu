// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// slsaDigestAnchor is the SLSA-provenance-derived provenance for one
// container image artifact digest: the build commit(s) and source
// repository(ies) a signed SLSA provenance predicate's config source (or, as
// a fallback, a git+-prefixed material) names for that digest's build. It
// mirrors ciRunDigestAnchor (container_image_identity_evidence.go), keyed by
// the CONTAINER IMAGE digest — the owning attestation.statement fact's
// single unambiguous subject_digest — rather than by image reference, so
// applySLSADigestRevision can attach it to whichever identity decision
// resolves that digest, including one raised by a different evidence source
// for the same image (#5456).
type slsaDigestAnchor struct {
	commits             []string
	sourceRepositoryIDs []string
	factIDs             []string
}

// extractSLSADigestAnchorsWithQuarantine builds the digest->commit anchor map
// from attestation.slsa_provenance facts, joined to their owning
// attestation.statement fact's subject digest. Two passes are required
// because the join key (the container image digest) lives on the STATEMENT,
// not the provenance predicate itself:
//
//  1. Decode every attestation.statement fact into statementID -> subjectDigest,
//     keeping only a statement with exactly ONE resolved subject digest — a
//     statement naming zero or multiple subjects is already the
//     sbom_attestation_attachment domain's SBOMAttachmentAmbiguousSubject
//     concern, so this domain simply contributes no anchor for it rather than
//     guessing.
//  2. Decode every attestation.slsa_provenance fact, look up its statement's
//     subject digest, extract a commit + source URL from the predicate's
//     config source (falling back to a git material), and record it under
//     that image digest.
//
// A required-field decode failure on either kind (a missing statement_id) is
// routed through partitionDecodeFailures for quarantine, matching every
// other extractor in this domain (container_image_identity_evidence.go,
// container_image_identity_typed_evidence.go).
func extractSLSADigestAnchorsWithQuarantine(
	envelopes []facts.Envelope,
) (map[string]slsaDigestAnchor, []quarantinedFact, error) {
	statementSubjects := map[string]string{}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AttestationStatementFactKind {
			continue
		}
		statement, err := decodeAttestationStatement(envelope)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if statement.StatementID == "" {
			continue
		}
		digests := uniqueSortedStrings(append(
			[]string{derefString(statement.SubjectDigest)},
			statement.SubjectDigests...,
		))
		if len(digests) == 1 {
			statementSubjects[statement.StatementID] = digests[0]
		}
	}
	if len(statementSubjects) == 0 {
		return nil, quarantined, nil
	}

	repositories := extractPackageSourceRepositories(envelopes)
	byDigest := map[string]slsaDigestAnchor{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AttestationSLSAProvenanceFactKind {
			continue
		}
		provenance, err := decodeAttestationSLSAProvenance(envelope)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		imageDigest := statementSubjects[provenance.StatementID]
		if imageDigest == "" {
			continue
		}
		commit, sourceURL := slsaProvenanceCommitAndSource(provenance)
		if commit == "" {
			continue
		}
		anchor := byDigest[imageDigest]
		anchor.commits = uniqueSortedStrings(append(anchor.commits, commit))
		anchor.factIDs = uniqueSortedStrings(append(anchor.factIDs, envelope.FactID))
		if sourceURL != "" {
			if match, ok := matchOCIConfigSourceRepository(sourceURL, repositories); ok {
				anchor.sourceRepositoryIDs = uniqueSortedStrings(append(anchor.sourceRepositoryIDs, match.RepositoryID))
			}
		}
		byDigest[imageDigest] = anchor
	}
	return byDigest, quarantined, nil
}

// slsaProvenanceCommitAndSource extracts a SLSA provenance predicate's build
// commit and source repository URL, preferring the build definition's config
// source (v1: buildDefinition.externalParameters.configSource; v0.2/v0.1:
// invocation.configSource — both decoded onto SLSAProvenance.ConfigSource by
// the collector) and falling back to a git+-prefixed material when the
// config source carries no commit digest. The commit is read from the
// "sha1" or "gitCommit" digest-algorithm key, matching the SLSA/in-toto git
// source convention.
func slsaProvenanceCommitAndSource(provenance sbomv1.SLSAProvenance) (commit string, sourceURL string) {
	if provenance.ConfigSource != nil {
		configSourceURL := derefString(provenance.ConfigSource.URI)
		if configSourceCommit := slsaGitCommitDigest(provenance.ConfigSource.Digest); configSourceCommit != "" {
			return configSourceCommit, slsaConfigSourceRepositoryURL(configSourceURL)
		}
	}
	for _, material := range provenance.Materials {
		uri := derefString(material.URI)
		if !strings.HasPrefix(uri, "git+") {
			continue
		}
		if materialCommit := slsaGitCommitDigest(material.Digest); materialCommit != "" {
			return materialCommit, slsaConfigSourceRepositoryURL(uri)
		}
	}
	return "", ""
}

// slsaGitCommitDigest reads a resolved git commit from a SLSA
// resource-descriptor digest map, accepting either the "sha1" key (the
// widely-used git-source convention) or "gitCommit" (an alternate algorithm
// name some SLSA producers use).
func slsaGitCommitDigest(digest map[string]string) string {
	return strings.ToLower(strings.TrimSpace(firstNonBlank(digest["sha1"], digest["gitCommit"])))
}

// slsaConfigSourceRepositoryURL strips a SLSA config source/material URI down
// to a bare repository URL matchOCIConfigSourceRepository (and, beneath it,
// repositoryidentity.NormalizedRemoteKey) can match against an active
// repository remote. NormalizedRemoteKey already strips a leading "git+", but
// the SLSA convention appends a ref suffix after the host+path
// ("git+https://host/path@refs/heads/main") that is not part of a git remote
// URL and would otherwise be read as part of the repository path, never
// matching a real remote_url — so the suffix after the first "@" following
// the scheme separator is cut here before normalization runs.
func slsaConfigSourceRepositoryURL(uri string) string {
	trimmed := strings.TrimSpace(uri)
	schemeIdx := strings.Index(trimmed, "://")
	if schemeIdx < 0 {
		return trimmed
	}
	rest := trimmed[schemeIdx+3:]
	if at := strings.Index(rest, "@"); at >= 0 {
		rest = rest[:at]
	}
	return trimmed[:schemeIdx+3] + rest
}
