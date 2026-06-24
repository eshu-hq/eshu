// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rds

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// instancePostureObservation derives the metadata-only posture observation for
// one RDS DB instance. Every value comes from already-reported describe
// metadata; the scanner never reads database contents to build it.
func instancePostureObservation(
	boundary awscloud.Boundary,
	instance DBInstance,
) awscloud.RDSPostureObservation {
	instanceARN := strings.TrimSpace(instance.ARN)
	identifier := strings.TrimSpace(instance.Identifier)
	resourceID := firstNonEmpty(instanceARN, instance.ResourceID, identifier)
	return awscloud.RDSPostureObservation{
		Boundary:                         boundary,
		ARN:                              instanceARN,
		ResourceID:                       resourceID,
		ResourceType:                     awscloud.ResourceTypeRDSDBInstance,
		Identifier:                       identifier,
		Engine:                           strings.TrimSpace(instance.Engine),
		PubliclyAccessible:               instance.PubliclyAccessible,
		StorageEncrypted:                 instance.StorageEncrypted,
		KMSKeyID:                         strings.TrimSpace(instance.KMSKeyID),
		IAMDatabaseAuthenticationEnabled: instance.IAMDatabaseAuthenticationEnabled,
		MultiAZ:                          instance.MultiAZ,
		DeletionProtection:               instance.DeletionProtection,
		BackupRetentionPeriod:            instance.BackupRetentionPeriod,
		PerformanceInsightsEnabled:       instance.PerformanceInsightsEnabled,
		PerformanceInsightsRetentionDays: instance.PerformanceInsightsRetentionDays,
		PerformanceInsightsKMSKeyID:      strings.TrimSpace(instance.PerformanceInsightsKMSKeyID),
		CACertificateIdentifier:          strings.TrimSpace(instance.CACertificateIdentifier),
		ParameterGroups:                  parameterGroupNames(instance.ParameterGroups),
		OptionGroups:                     optionGroupNames(instance.OptionGroups),
		SecurityParameters:               cloneStringMap(instance.SecurityParameters),
		SourceRecordID:                   resourceID,
	}
}

// clusterPostureObservation derives the metadata-only posture observation for
// one Aurora DB cluster. Every value comes from already-reported describe
// metadata; the scanner never reads database contents to build it.
func clusterPostureObservation(
	boundary awscloud.Boundary,
	cluster DBCluster,
) awscloud.RDSPostureObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	identifier := strings.TrimSpace(cluster.Identifier)
	resourceID := firstNonEmpty(clusterARN, cluster.ResourceID, identifier)
	var parameterGroups []string
	if name := strings.TrimSpace(cluster.ParameterGroup); name != "" {
		parameterGroups = []string{name}
	}
	return awscloud.RDSPostureObservation{
		Boundary:                         boundary,
		ARN:                              clusterARN,
		ResourceID:                       resourceID,
		ResourceType:                     awscloud.ResourceTypeRDSDBCluster,
		Identifier:                       identifier,
		Engine:                           strings.TrimSpace(cluster.Engine),
		PubliclyAccessible:               cluster.PubliclyAccessible,
		StorageEncrypted:                 cluster.StorageEncrypted,
		KMSKeyID:                         strings.TrimSpace(cluster.KMSKeyID),
		IAMDatabaseAuthenticationEnabled: cluster.IAMDatabaseAuthenticationEnabled,
		MultiAZ:                          cluster.MultiAZ,
		DeletionProtection:               cluster.DeletionProtection,
		BackupRetentionPeriod:            cluster.BackupRetentionPeriod,
		PerformanceInsightsEnabled:       cluster.PerformanceInsightsEnabled,
		PerformanceInsightsRetentionDays: cluster.PerformanceInsightsRetentionDays,
		PerformanceInsightsKMSKeyID:      strings.TrimSpace(cluster.PerformanceInsightsKMSKeyID),
		ParameterGroups:                  parameterGroups,
		SecurityParameters:               cloneStringMap(cluster.SecurityParameters),
		SourceRecordID:                   resourceID,
	}
}

func parameterGroupNames(groups []ParameterGroup) []string {
	if len(groups) == 0 {
		return nil
	}
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		if name := strings.TrimSpace(group.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func optionGroupNames(groups []OptionGroup) []string {
	if len(groups) == 0 {
		return nil
	}
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		if name := strings.TrimSpace(group.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
