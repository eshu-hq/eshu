// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// Request describes one `eshu investigation export` run after the CLI has read
// its flags: which family to build, the scope the operator named, and an
// optional bounds override (nil means the contract defaults apply).
type Request struct {
	Family  query.InvestigationFamily
	Subject map[string]string
	Bounds  *query.PacketBounds
}

// BuildPacket dispatches by family and returns the artifact to render.
//
// Two outcomes are both "success" from the caller's point of view. A packet
// whose Refusal is set is a valid, share-safe artifact saying why the question
// could not be answered -- an unrecognized family, a scope the API cannot
// resolve, an unavailable backend. A returned error means the CLI could not
// produce any honest artifact, and the operator sees the message instead.
//
// The default branch covers a family the contract recognizes but this build has
// no reader for. It is an error rather than a refusal on purpose: an empty
// artifact would read as "nothing found" when the truth is "not implemented
// here".
func BuildPacket(client Client, deps Deps, req Request) (query.InvestigationEvidencePacket, error) {
	if !query.ValidInvestigationFamily(req.Family) {
		return refusalPacket(req.Family, req.Subject, query.PacketRefusalUnknownFamily)
	}
	switch req.Family {
	case query.InvestigationFamilySupplyChainImpact:
		return buildSupplyChainPacket(client, deps, req)
	case query.InvestigationFamilyDeployableUnit:
		return buildDeployableUnitPacket(client, deps, req)
	case query.InvestigationFamilyDrift:
		return buildDriftPacket(client, deps, req)
	default:
		return query.InvestigationEvidencePacket{}, fmt.Errorf(
			"investigation family %q is recognized but not yet available in this CLI build", req.Family,
		)
	}
}

// buildSupplyChainPacket reads the impact explanation for the requested advisory
// or finding and maps it into the v2 packet.
func buildSupplyChainPacket(client Client, deps Deps, req Request) (query.InvestigationEvidencePacket, error) {
	filter := SupplyChainFilterFromSubject(req.Subject)
	if !SupplyChainFilterHasScope(filter) {
		return refusalPacket(query.InvestigationFamilySupplyChainImpact, req.Subject, query.PacketRefusalScopeNotFound)
	}
	envelope, err := deps.FetchSupplyChainExplain(client, filter)
	if err != nil {
		if refusal, refused := RefusalFromFetchError(err); refused {
			return refusalPacket(query.InvestigationFamilySupplyChainImpact, req.Subject, refusal)
		}
		// Unwrapped so the operator reads the client's own text; wrapcheck does
		// not flag this one (the error comes from a struct field call).
		return query.InvestigationEvidencePacket{}, err
	}
	if refusal, refused, err := RefusalFromEnvelopeError(envelope.Error); err != nil {
		return query.InvestigationEvidencePacket{}, err
	} else if refused {
		return refusalPacket(query.InvestigationFamilySupplyChainImpact, req.Subject, refusal)
	}
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return query.BuildSupplyChainImpactPacket(envelope.Data, envelope.Truth, req.Bounds)
}

// buildDeployableUnitPacket reads bounded deployable-unit admission decisions for
// the requested scope and maps them into the v2 packet. A missing scope or a
// not-found/backend error yields a refusal packet.
func buildDeployableUnitPacket(client Client, deps Deps, req Request) (query.InvestigationEvidencePacket, error) {
	params, ok := DeployableUnitParams(req.Subject)
	if !ok {
		return refusalPacket(query.InvestigationFamilyDeployableUnit, req.Subject, query.PacketRefusalScopeNotFound)
	}
	envelope, err := deps.FetchAdmissionDecisions(client, params)
	if err != nil {
		if refusal, refused := RefusalFromFetchError(err); refused {
			return refusalPacket(query.InvestigationFamilyDeployableUnit, req.Subject, refusal)
		}
		// Unwrapped so the operator reads the client's own text; wrapcheck does
		// not flag this one (the error comes from a struct field call).
		return query.InvestigationEvidencePacket{}, err
	}
	if refusal, refused, err := RefusalFromEnvelopeError(envelope.Error); err != nil {
		return query.InvestigationEvidencePacket{}, err
	} else if refused {
		return refusalPacket(query.InvestigationFamilyDeployableUnit, req.Subject, refusal)
	}
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return query.BuildDeployableUnitPacket(envelope.Data.Decisions, req.Subject, envelope.Truth, req.Bounds)
}

// buildDriftPacket reads bounded cloud runtime drift findings for the requested
// scope and maps them into the v2 packet.
func buildDriftPacket(client Client, deps Deps, req Request) (query.InvestigationEvidencePacket, error) {
	body, ok := DriftRequestBody(req.Subject)
	if !ok {
		return refusalPacket(query.InvestigationFamilyDrift, req.Subject, query.PacketRefusalScopeNotFound)
	}
	envelope, err := deps.FetchDriftFindings(client, body)
	if err != nil {
		if refusal, refused := RefusalFromFetchError(err); refused {
			return refusalPacket(query.InvestigationFamilyDrift, req.Subject, refusal)
		}
		// Unwrapped so the operator reads the client's own text; wrapcheck does
		// not flag this one (the error comes from a struct field call).
		return query.InvestigationEvidencePacket{}, err
	}
	if refusal, refused, err := RefusalFromEnvelopeError(envelope.Error); err != nil {
		return query.InvestigationEvidencePacket{}, err
	} else if refused {
		return refusalPacket(query.InvestigationFamilyDrift, req.Subject, refusal)
	}
	//nolint:wrapcheck // the packet contract's own message is what the operator needs
	return query.BuildDriftPacket(envelope.Data.DriftFindings, req.Subject, envelope.Truth, req.Bounds)
}
