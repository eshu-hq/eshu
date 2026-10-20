// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appmesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS App Mesh metadata facts for one claimed account and region.
//
// The scanner is metadata only. It never calls an App Mesh mutation API, never
// reads or persists a client TLS certificate body, and redacts sensitive HTTP
// header match values through the shared redact library before emission. It
// requires a redaction key for that reason and fails closed when the key is
// zero.
type Scanner struct {
	// Client is the metadata-only App Mesh read surface.
	Client Client
	// RedactionKey produces deterministic markers for sensitive HTTP header
	// match values. A zero key is a configuration error, not a silent fallback.
	RedactionKey redact.Key
}

// Scan observes App Mesh resources through the configured client and emits
// metadata-only resource facts plus the relationships App Mesh reports
// directly. Errors from the client are wrapped so partial failures surface
// rather than producing a silently truncated inventory.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("appmesh scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("appmesh scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAppMesh:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAppMesh
	default:
		return nil, fmt.Errorf("appmesh scanner received service_kind %q", boundary.ServiceKind)
	}

	meshes, err := s.Client.ListMeshInventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("list App Mesh inventory: %w", err)
	}

	var envelopes []facts.Envelope
	for _, mesh := range meshes {
		meshEnvelopes, err := s.meshEnvelopes(boundary, mesh)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, meshEnvelopes...)
	}
	return envelopes, nil
}

// meshEnvelopes emits the mesh resource and every child resource and
// relationship the mesh reports.
func (s Scanner) meshEnvelopes(boundary awscloud.Boundary, mesh Mesh) ([]facts.Envelope, error) {
	var envelopes []facts.Envelope
	if err := appendResource(&envelopes, meshObservation(boundary, mesh)); err != nil {
		return nil, err
	}

	for _, service := range mesh.VirtualServices {
		if err := appendResource(&envelopes, virtualServiceObservation(boundary, service)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, virtualServiceRelationships(boundary, mesh, service)); err != nil {
			return nil, err
		}
	}

	for _, node := range mesh.VirtualNodes {
		if err := appendResource(&envelopes, virtualNodeObservation(boundary, node)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, virtualNodeRelationships(boundary, node)); err != nil {
			return nil, err
		}
	}

	for _, router := range mesh.VirtualRouters {
		if err := appendResource(&envelopes, virtualRouterObservation(boundary, router)); err != nil {
			return nil, err
		}
		for _, route := range router.Routes {
			if err := appendResource(&envelopes, s.routeObservation(boundary, route)); err != nil {
				return nil, err
			}
			if err := appendRelationships(&envelopes, routeRelationships(boundary, route)); err != nil {
				return nil, err
			}
		}
	}

	for _, gateway := range mesh.VirtualGateways {
		if err := appendResource(&envelopes, virtualGatewayObservation(boundary, gateway)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, virtualGatewayRelationships(boundary, mesh, gateway)); err != nil {
			return nil, err
		}
		for _, route := range gateway.GatewayRoutes {
			if err := appendResource(&envelopes, gatewayRouteObservation(boundary, route)); err != nil {
				return nil, err
			}
		}
	}

	return envelopes, nil
}

func appendResource(envelopes *[]facts.Envelope, observation awscloud.ResourceObservation) error {
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationships(envelopes *[]facts.Envelope, observations []awscloud.RelationshipObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}
