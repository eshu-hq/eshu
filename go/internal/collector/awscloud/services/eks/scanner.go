package eks

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits EKS cluster, nodegroup, add-on, OIDC provider, and
// relationship facts for one claimed account and region.
type Scanner struct {
	Client Client
}

// Scan observes EKS resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("eks scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceEKS
	case awscloud.ServiceEKS:
	default:
		return nil, fmt.Errorf("eks scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EKS clusters: %w", err)
	}
	var envelopes []facts.Envelope
	for _, cluster := range clusters {
		clusterEnvelopes, err := s.clusterEnvelopes(ctx, boundary, cluster)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, clusterEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) clusterEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	cluster Cluster,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if cluster.OIDCProvider != nil {
		oidcEnvelopes, err := oidcProviderEnvelopes(boundary, cluster, *cluster.OIDCProvider)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, oidcEnvelopes...)
	}
	for _, observation := range clusterRelationships(boundary, cluster) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}

	nodegroups, err := s.Client.ListNodegroups(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("list EKS nodegroups for cluster %q: %w", cluster.Name, err)
	}
	for _, nodegroup := range nodegroups {
		nodegroupEnvelopes, err := nodegroupEnvelopes(boundary, cluster, nodegroup)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, nodegroupEnvelopes...)
	}

	addons, err := s.Client.ListAddons(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("list EKS addons for cluster %q: %w", cluster.Name, err)
	}
	for _, addon := range addons {
		addonEnvelopes, err := addonEnvelopes(boundary, cluster, addon)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, addonEnvelopes...)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   firstNonEmpty(clusterARN, cluster.Name),
		ResourceType: awscloud.ResourceTypeEKSCluster,
		Name:         strings.TrimSpace(cluster.Name),
		State:        strings.TrimSpace(cluster.Status),
		Tags:         cluster.Tags,
		Attributes: map[string]any{
			"created_at":       timeOrNil(cluster.CreatedAt),
			"endpoint":         strings.TrimSpace(cluster.Endpoint),
			"oidc_provider":    oidcProviderMap(cluster.OIDCProvider),
			"platform_version": strings.TrimSpace(cluster.PlatformVersion),
			"role_arn":         strings.TrimSpace(cluster.RoleARN),
			"version":          strings.TrimSpace(cluster.Version),
			"vpc_config":       vpcConfigMap(cluster.VPCConfig),
		},
		CorrelationAnchors: []string{clusterARN, strings.TrimSpace(cluster.Name)},
		SourceRecordID:     firstNonEmpty(clusterARN, cluster.Name),
	}
}

func oidcProviderMap(provider *OIDCProvider) map[string]any {
	if provider == nil {
		return nil
	}
	return map[string]any{
		"arn":         strings.TrimSpace(provider.ARN),
		"client_ids":  cloneStrings(provider.ClientIDs),
		"issuer_url":  strings.TrimSpace(provider.IssuerURL),
		"thumbprints": cloneStrings(provider.Thumbprints),
	}
}

func oidcProviderEnvelopes(
	boundary awscloud.Boundary,
	cluster Cluster,
	provider OIDCProvider,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(oidcProviderObservation(boundary, cluster, provider))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := clusterOIDCProviderRelationship(boundary, cluster, provider); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func oidcProviderObservation(
	boundary awscloud.Boundary,
	cluster Cluster,
	provider OIDCProvider,
) awscloud.ResourceObservation {
	providerID := firstNonEmpty(provider.ARN, provider.IssuerURL)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(provider.ARN),
		ResourceID:   providerID,
		ResourceType: awscloud.ResourceTypeEKSOIDCProvider,
		Name:         strings.TrimSpace(provider.IssuerURL),
		Attributes: map[string]any{
			"cluster_arn":  strings.TrimSpace(cluster.ARN),
			"cluster_name": strings.TrimSpace(cluster.Name),
			"client_ids":   cloneStrings(provider.ClientIDs),
			"issuer_url":   strings.TrimSpace(provider.IssuerURL),
			"thumbprints":  cloneStrings(provider.Thumbprints),
		},
		CorrelationAnchors: []string{providerID, strings.TrimSpace(provider.IssuerURL), strings.TrimSpace(cluster.Name)},
		SourceRecordID:     providerID,
	}
}
