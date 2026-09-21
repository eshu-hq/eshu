// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudhsmv2

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS CloudHSM v2 metadata-only facts for one claimed account and
// region. It reports clusters (with embedded HSM ENI placement and certificate
// presence flags) and backups, plus the cluster-in-VPC, cluster-in-subnet,
// cluster-uses-security-group, and backup-of-cluster relationships. It never
// reads, writes, or persists cryptographic key material, certificate PEM
// bodies, the cluster certificate signing request body, or the cluster's
// Pre-Crypto Officer password.
type Scanner struct {
	// Client is the metadata-only CloudHSM v2 snapshot source.
	Client Client
}

// Scan observes CloudHSM v2 clusters, their HSM and certificate-presence
// metadata, their backups, and the network and backup relationships through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cloudhsmv2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCloudHSMV2:
		boundary.ServiceKind = awscloud.ServiceCloudHSMV2
	default:
		return nil, fmt.Errorf("cloudhsmv2 scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot CloudHSM v2 metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, cluster := range snapshot.Clusters {
		next, err := clusterEnvelopes(boundary, cluster)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, backup := range snapshot.Backups {
		next, err := backupEnvelopes(boundary, backup)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func clusterEnvelopes(boundary awscloud.Boundary, cluster Cluster) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	relationships := []*awscloud.RelationshipObservation{
		clusterVPCRelationship(boundary, cluster),
		clusterSecurityGroupRelationship(boundary, cluster),
	}
	for _, subnet := range clusterSubnetRelationships(boundary, cluster) {
		subnet := subnet
		relationships = append(relationships, &subnet)
	}
	for _, relationship := range relationships {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func backupEnvelopes(boundary awscloud.Boundary, backup Backup) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(backupObservation(boundary, backup))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := backupClusterRelationship(boundary, backup); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	resourceID := clusterResourceID(cluster)
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeCloudHSMV2Cluster,
		Name:               resourceID,
		State:              strings.TrimSpace(cluster.State),
		Tags:               cloneStringMap(cluster.Tags),
		Attributes:         clusterAttributes(cluster),
		CorrelationAnchors: correlationAnchors(resourceID),
		SourceRecordID:     resourceID,
	}
}

func clusterAttributes(cluster Cluster) map[string]any {
	attributes := map[string]any{
		"cluster_id":       clusterResourceID(cluster),
		"state":            strings.TrimSpace(cluster.State),
		"state_message":    strings.TrimSpace(cluster.StateMessage),
		"hsm_type":         strings.TrimSpace(cluster.HsmType),
		"mode":             strings.TrimSpace(cluster.Mode),
		"network_type":     strings.TrimSpace(cluster.NetworkType),
		"vpc_id":           strings.TrimSpace(cluster.VPCID),
		"security_group":   strings.TrimSpace(cluster.SecurityGroupID),
		"source_backup_id": strings.TrimSpace(cluster.SourceBackupID),
		"backup_policy":    strings.TrimSpace(cluster.BackupPolicy),
		"hsm_count":        len(cluster.HSMs),
		"create_timestamp": timeOrNil(cluster.CreateTimestamp),
		// Certificate presence only; bodies are never read or persisted.
		"cluster_certificate_present":               cluster.CertificatePresence.ClusterCertificate,
		"cluster_csr_present":                       cluster.CertificatePresence.ClusterCSR,
		"hsm_certificate_present":                   cluster.CertificatePresence.HSMCertificate,
		"aws_hardware_certificate_present":          cluster.CertificatePresence.AWSHardwareCertificate,
		"manufacturer_hardware_certificate_present": cluster.CertificatePresence.ManufacturerHardwareCertificate,
	}
	if retentionType := strings.TrimSpace(cluster.BackupRetentionType); retentionType != "" {
		attributes["backup_retention_type"] = retentionType
	}
	if retentionValue := strings.TrimSpace(cluster.BackupRetentionValue); retentionValue != "" {
		attributes["backup_retention_value"] = retentionValue
	}
	if subnets := subnetIDs(cluster); len(subnets) > 0 {
		attributes["subnet_ids"] = subnets
	}
	if hsms := hsmAttributes(cluster.HSMs); len(hsms) > 0 {
		attributes["hsms"] = hsms
	}
	return attributes
}

// subnetIDs returns the de-duplicated bare subnet ids the cluster spans, sourced
// from the availability-zone-to-subnet mapping. It mirrors the subnet edge set.
func subnetIDs(cluster Cluster) []string {
	var ids []string
	seen := map[string]struct{}{}
	for _, mapping := range cluster.SubnetMappings {
		subnetID := strings.TrimSpace(mapping.SubnetID)
		if subnetID == "" {
			continue
		}
		if _, exists := seen[subnetID]; exists {
			continue
		}
		seen[subnetID] = struct{}{}
		ids = append(ids, subnetID)
	}
	return ids
}

// hsmAttributes renders the HSM ENI placement metadata as attribute maps. It
// carries only id, state, zone, subnet, and ENI addressing; no key material is
// reachable from an HSM record.
func hsmAttributes(hsms []HSM) []map[string]any {
	if len(hsms) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(hsms))
	for _, hsm := range hsms {
		entry := map[string]any{
			"hsm_id":            strings.TrimSpace(hsm.ID),
			"state":             strings.TrimSpace(hsm.State),
			"availability_zone": strings.TrimSpace(hsm.AvailabilityZone),
			"subnet_id":         strings.TrimSpace(hsm.SubnetID),
			"eni_id":            strings.TrimSpace(hsm.ENIID),
			"eni_ip":            strings.TrimSpace(hsm.ENIIP),
		}
		if ipv6 := strings.TrimSpace(hsm.ENIIPV6); ipv6 != "" {
			entry["eni_ipv6"] = ipv6
		}
		out = append(out, entry)
	}
	return out
}

func backupObservation(boundary awscloud.Boundary, backup Backup) awscloud.ResourceObservation {
	resourceID := backupResourceID(backup)
	arn := strings.TrimSpace(backup.ARN)
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeCloudHSMV2Backup,
		Name:               resourceID,
		State:              strings.TrimSpace(backup.State),
		Tags:               cloneStringMap(backup.Tags),
		Attributes:         backupAttributes(backup),
		CorrelationAnchors: correlationAnchors(arn, resourceID),
		SourceRecordID:     resourceID,
	}
}

func backupAttributes(backup Backup) map[string]any {
	attributes := map[string]any{
		"backup_id":        backupResourceID(backup),
		"state":            strings.TrimSpace(backup.State),
		"cluster_id":       strings.TrimSpace(backup.ClusterID),
		"hsm_type":         strings.TrimSpace(backup.HsmType),
		"mode":             strings.TrimSpace(backup.Mode),
		"never_expires":    backup.NeverExpires,
		"create_timestamp": timeOrNil(backup.CreateTimestamp),
	}
	if sourceBackup := strings.TrimSpace(backup.SourceBackup); sourceBackup != "" {
		attributes["source_backup_id"] = sourceBackup
	}
	if sourceCluster := strings.TrimSpace(backup.SourceCluster); sourceCluster != "" {
		attributes["source_cluster_id"] = sourceCluster
	}
	if sourceRegion := strings.TrimSpace(backup.SourceRegion); sourceRegion != "" {
		attributes["source_region"] = sourceRegion
	}
	if copied := timeOrNil(backup.CopyTimestamp); copied != nil {
		attributes["copy_timestamp"] = copied
	}
	if deleted := timeOrNil(backup.DeleteTimestamp); deleted != nil {
		attributes["delete_timestamp"] = deleted
	}
	return attributes
}

// correlationAnchors returns the trimmed, non-empty identity anchors for a
// resource, dropping blanks so the payload carries only real join keys.
func correlationAnchors(values ...string) []string {
	var anchors []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			anchors = append(anchors, trimmed)
		}
	}
	return anchors
}
