// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// stubContainerImageIdentityFactLoader and recordingContainerImageIdentityWriter
// are the shared FactLoader/Writer test doubles every container_image_identity
// test file in this package (container_image_identity_test.go and its
// sibling *_test.go files covering CI, SLSA, ECS, derived-from, and
// commit-revision fixtures) constructs directly. Split into their own file so
// container_image_identity_test.go stays under the repository's 500-line cap
// as those sibling fixtures grow.
type stubContainerImageIdentityFactLoader struct {
	scopeFacts []facts.Envelope
	active     []facts.Envelope
	kindCalls  [][]string
	activeCall int
	// slsaActive and slsaActiveCall back the #5456 PR #5707 P1-b cross-scope
	// SLSA/verification loader (activeContainerImageSLSAFactLoader): they
	// simulate attestation.statement/slsa_provenance/signature_verification
	// facts living in a DIFFERENT scope than the one under test, reachable
	// only through this loader — mirroring how active/activeCall already
	// simulate the cross-scope OCI/content_entity loader.
	slsaActive     []facts.Envelope
	slsaActiveCall int
	// ciActive and ciActiveCall back the #5810 cross-scope CI loader
	// (activeContainerImageCIFactLoader): they simulate ci.run/ci.artifact
	// facts living in the CI run's OWN scope, reachable only through this
	// loader — mirroring slsaActive/slsaActiveCall above. ciActiveOwnerRepositoryIDs
	// records every ownerRepositoryID this stub was called with (#5810 P1
	// follow-up: the real Postgres loader now takes the owner as an argument
	// and pushes it into SQL) so a test can assert Handle threaded the
	// correct owner through without needing a live database. The stub
	// deliberately does NOT filter ciActive by the passed owner itself --
	// it returns the fixture verbatim, matching what a real unfiltered
	// FactLoader implementation would look like, so existing fixtures that
	// rely on the reducer's OWN filterContainerImageCIFactsForOwner
	// safety net stay valid unchanged.
	ciActive                   []facts.Envelope
	ciActiveCall               int
	ciActiveOwnerRepositoryIDs []string
	warnings                   []facts.Envelope
	warningCalls               int
	warningErr                 error
}

func (s *stubContainerImageIdentityFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageIdentityFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.activeCall++
	return append([]facts.Envelope(nil), s.active...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageIdentityWarnings(
	context.Context,
) ([]facts.Envelope, error) {
	s.warningCalls++
	return append([]facts.Envelope(nil), s.warnings...), s.warningErr
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageSLSAFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.slsaActiveCall++
	return append([]facts.Envelope(nil), s.slsaActive...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageCIFacts(
	_ context.Context,
	ownerRepositoryID string,
) ([]facts.Envelope, error) {
	s.ciActiveCall++
	s.ciActiveOwnerRepositoryIDs = append(s.ciActiveOwnerRepositoryIDs, ownerRepositoryID)
	return append([]facts.Envelope(nil), s.ciActive...), nil
}

type recordingContainerImageIdentityWriter struct {
	write ContainerImageIdentityWrite
	calls int
	err   error
}

func (w *recordingContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	_ context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	w.calls++
	w.write = write
	if w.err != nil {
		return ContainerImageIdentityWriteResult{}, w.err
	}
	return ContainerImageIdentityWriteResult{
		CanonicalWrites: len(containerImageIdentityCanonicalDecisions(write.Decisions)),
	}, nil
}
