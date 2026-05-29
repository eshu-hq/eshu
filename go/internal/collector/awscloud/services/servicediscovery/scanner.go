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
	case "":
		boundary.ServiceKind = awscloud.ServiceServiceDiscovery
	case awscloud.ServiceServiceDiscovery:
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
func namespaceEnvelopes(boundary awscloud.Boundary, namespace Namespace) ([]facts.Envelope, error) {
	var envelopes []facts.Envelope
	if err := appendResource(&envelopes, namespaceObservation(boundary, namespace)); err != nil {
		return nil, err
	}
	if err := appendRelationships(&envelopes, namespaceRelationships(boundary, namespace)); err != nil {
		return nil, err
	}

	for _, service := range namespace.Services {
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
