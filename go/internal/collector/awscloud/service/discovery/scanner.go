// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicediscovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Cloud Map (Service Discovery) metadata facts for one
// claimed account and region.
//
// The scanner is metadata only. It never calls a Cloud Map mutation API, never
// reads or persists instance attribute maps (which can carry caller-defined
// secrets), and never resolves instance values. It records instance counts
// only, from the Cloud Map service summary.
type Scanner struct {
	// Client is the metadata-only Cloud Map read surface.
	Client Client
}

// Scan observes Cloud Map namespaces and services through the configured client
// and emits metadata-only resource facts plus the relationships Cloud Map
// reports directly. Errors from the client are wrapped so partial failures
// surface rather than producing a silently truncated inventory.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("servicediscovery scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceServiceDiscovery:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceServiceDiscovery
	default:
		return nil, fmt.Errorf("servicediscovery scanner received service_kind %q", boundary.ServiceKind)
	}

	namespaces, err := s.Client.ListNamespaceInventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Cloud Map inventory: %w", err)
	}

	var envelopes []facts.Envelope
	for _, namespace := range namespaces {
		namespaceEnvelopes, err := namespaceEnvelopes(boundary, namespace)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, namespaceEnvelopes...)
	}
	return envelopes, nil
}

// namespaceEnvelopes emits the namespace resource, its hosted-zone edge (DNS
// namespaces only), and every service resource and service-to-namespace edge
// the namespace reports.
//
// A namespace with a blank id is skipped entirely: the namespace resource is
// keyed by the Cloud Map namespace id, so without it NewResourceEnvelope would
// fall back to the ARN as resource_id while the namespace_id attribute stayed
// blank, and every service-to-namespace edge would target a key that cannot
// join. A service whose "namespaceName/serviceName" join key cannot be formed
// is likewise skipped so the scanner never emits a Cloud Map service keyed on
// an ARN, which would break the App Mesh virtual-node-to-Cloud-Map-service edge.
func namespaceEnvelopes(boundary awscloud.Boundary, namespace Namespace) ([]facts.Envelope, error) {
	if strings.TrimSpace(namespace.ID) == "" {
		return nil, nil
	}

	var envelopes []facts.Envelope
	if err := appendResource(&envelopes, namespaceObservation(boundary, namespace)); err != nil {
		return nil, err
	}
	if err := appendRelationships(&envelopes, namespaceRelationships(boundary, namespace)); err != nil {
		return nil, err
	}

	for _, service := range namespace.Services {
		if serviceResourceID(service) == "" {
			continue
		}
		if err := appendResource(&envelopes, serviceObservation(boundary, service)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, serviceRelationships(boundary, service)); err != nil {
			return nil, err
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
