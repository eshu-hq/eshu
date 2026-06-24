// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdmstypes "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice/types"

	dmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dms"
)

// mapSubnetGroup converts an SDK replication subnet group into the scanner-owned
// metadata view, recording the VPC id and member subnet ids only.
func mapSubnetGroup(group awsdmstypes.ReplicationSubnetGroup) dmsservice.ReplicationSubnetGroup {
	return dmsservice.ReplicationSubnetGroup{
		Identifier:  strings.TrimSpace(aws.ToString(group.ReplicationSubnetGroupIdentifier)),
		Description: strings.TrimSpace(aws.ToString(group.ReplicationSubnetGroupDescription)),
		Status:      strings.TrimSpace(aws.ToString(group.SubnetGroupStatus)),
		VPCID:       strings.TrimSpace(aws.ToString(group.VpcId)),
		SubnetIDs:   subnetIDs(group.Subnets),
	}
}

// indexSubnetGroups maps a subnet-group identifier to its scanner-owned view so
// a replication instance can inherit the VPC and subnet ids reported on its
// embedded subnet group without a second describe call.
func indexSubnetGroups(groups []dmsservice.ReplicationSubnetGroup) map[string]dmsservice.ReplicationSubnetGroup {
	index := make(map[string]dmsservice.ReplicationSubnetGroup, len(groups))
	for _, group := range groups {
		identifier := strings.TrimSpace(group.Identifier)
		if identifier == "" {
			continue
		}
		index[identifier] = group
	}
	return index
}

// buildInstance converts an SDK replication instance into the scanner-owned
// metadata view. It derives the subnet group identity, VPC, and subnets from
// the instance's embedded subnet group (falling back to the indexed subnet
// group when the embedded one omits them), and records the VPC security group
// ids attached to the instance.
func buildInstance(
	instance awsdmstypes.ReplicationInstance,
	subnetGroups map[string]dmsservice.ReplicationSubnetGroup,
	tags map[string]string,
) dmsservice.ReplicationInstance {
	mapped := dmsservice.ReplicationInstance{
		ARN:                 strings.TrimSpace(aws.ToString(instance.ReplicationInstanceArn)),
		Identifier:          strings.TrimSpace(aws.ToString(instance.ReplicationInstanceIdentifier)),
		Class:               strings.TrimSpace(aws.ToString(instance.ReplicationInstanceClass)),
		EngineVersion:       strings.TrimSpace(aws.ToString(instance.EngineVersion)),
		Status:              strings.TrimSpace(aws.ToString(instance.ReplicationInstanceStatus)),
		AllocatedStorageGiB: instance.AllocatedStorage,
		MultiAZ:             instance.MultiAZ,
		PubliclyAccessible:  instance.PubliclyAccessible,
		AvailabilityZone:    strings.TrimSpace(aws.ToString(instance.AvailabilityZone)),
		NetworkType:         strings.TrimSpace(aws.ToString(instance.NetworkType)),
		KMSKeyID:            strings.TrimSpace(aws.ToString(instance.KmsKeyId)),
		SecurityGroupIDs:    securityGroupIDs(instance.VpcSecurityGroups),
		CreateTime:          aws.ToTime(instance.InstanceCreateTime),
		Tags:                tags,
	}
	applySubnetGroup(&mapped, instance.ReplicationSubnetGroup, subnetGroups)
	return mapped
}

// applySubnetGroup records the instance's subnet group identifier, VPC id, and
// member subnet ids. It prefers the values on the instance's embedded subnet
// group and falls back to the indexed subnet group for any field the embedded
// copy omits, so the instance's subnet and VPC edges resolve.
func applySubnetGroup(
	instance *dmsservice.ReplicationInstance,
	embedded *awsdmstypes.ReplicationSubnetGroup,
	subnetGroups map[string]dmsservice.ReplicationSubnetGroup,
) {
	if embedded == nil {
		return
	}
	identifier := strings.TrimSpace(aws.ToString(embedded.ReplicationSubnetGroupIdentifier))
	instance.SubnetGroupIdentifier = identifier
	instance.VPCID = strings.TrimSpace(aws.ToString(embedded.VpcId))
	instance.SubnetIDs = subnetIDs(embedded.Subnets)
	indexed, ok := subnetGroups[identifier]
	if !ok {
		return
	}
	if instance.VPCID == "" {
		instance.VPCID = indexed.VPCID
	}
	if len(instance.SubnetIDs) == 0 {
		instance.SubnetIDs = append([]string(nil), indexed.SubnetIDs...)
	}
}

// mapEndpoint converts an SDK endpoint into the scanner-owned metadata view. It
// records identity, engine, SSL mode, status, and resolvable data-store and
// secret references only. It never reads the server name (a credential),
// username, password, connection attributes, external table definition, or SSL
// key material.
func mapEndpoint(endpoint awsdmstypes.Endpoint) dmsservice.Endpoint {
	mapped := dmsservice.Endpoint{
		ARN:                    strings.TrimSpace(aws.ToString(endpoint.EndpointArn)),
		Identifier:             strings.TrimSpace(aws.ToString(endpoint.EndpointIdentifier)),
		EndpointType:           strings.TrimSpace(string(endpoint.EndpointType)),
		EngineName:             strings.TrimSpace(aws.ToString(endpoint.EngineName)),
		EngineDisplayName:      strings.TrimSpace(aws.ToString(endpoint.EngineDisplayName)),
		SSLMode:                strings.TrimSpace(string(endpoint.SslMode)),
		Status:                 strings.TrimSpace(aws.ToString(endpoint.Status)),
		DatabaseName:           strings.TrimSpace(aws.ToString(endpoint.DatabaseName)),
		Port:                   aws.ToInt32(endpoint.Port),
		KMSKeyID:               strings.TrimSpace(aws.ToString(endpoint.KmsKeyId)),
		KinesisStreamARN:       kinesisStreamARN(endpoint.KinesisSettings),
		S3BucketName:           s3BucketName(endpoint.S3Settings),
		SecretsManagerSecretID: endpointSecretID(endpoint),
	}
	return mapped
}

// buildTask converts an SDK replication task into the scanner-owned metadata
// view, recording the source/target endpoint and replication instance ARN
// references only. It never reads the task settings or table-mapping body.
func buildTask(task awsdmstypes.ReplicationTask, tags map[string]string) dmsservice.ReplicationTask {
	return dmsservice.ReplicationTask{
		ARN:                    strings.TrimSpace(aws.ToString(task.ReplicationTaskArn)),
		Identifier:             strings.TrimSpace(aws.ToString(task.ReplicationTaskIdentifier)),
		MigrationType:          strings.TrimSpace(string(task.MigrationType)),
		Status:                 strings.TrimSpace(aws.ToString(task.Status)),
		SourceEndpointARN:      strings.TrimSpace(aws.ToString(task.SourceEndpointArn)),
		TargetEndpointARN:      strings.TrimSpace(aws.ToString(task.TargetEndpointArn)),
		ReplicationInstanceARN: strings.TrimSpace(aws.ToString(task.ReplicationInstanceArn)),
		CreationDate:           aws.ToTime(task.ReplicationTaskCreationDate),
		Tags:                   tags,
	}
}

func subnetIDs(subnets []awsdmstypes.Subnet) []string {
	if len(subnets) == 0 {
		return nil
	}
	ids := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.SubnetIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func securityGroupIDs(groups []awsdmstypes.VpcSecurityGroupMembership) []string {
	if len(groups) == 0 {
		return nil
	}
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.VpcSecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func kinesisStreamARN(settings *awsdmstypes.KinesisSettings) string {
	if settings == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(settings.StreamArn))
}

func s3BucketName(settings *awsdmstypes.S3Settings) string {
	if settings == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(settings.BucketName))
}

// endpointSecretID returns the Secrets Manager secret reference DMS reports for
// the endpoint's connection credentials, reading it from whichever engine
// settings struct is populated. The secret value is never read; only the secret
// id/ARN reference is recorded. It returns "" when the endpoint uses inline
// credentials or no secret.
func endpointSecretID(endpoint awsdmstypes.Endpoint) string {
	candidates := []*string{
		secretIDFromMySQL(endpoint.MySQLSettings),
		secretIDFromPostgreSQL(endpoint.PostgreSQLSettings),
		secretIDFromOracle(endpoint.OracleSettings),
		secretIDFromSQLServer(endpoint.MicrosoftSQLServerSettings),
		secretIDFromMongoDB(endpoint.MongoDbSettings),
		secretIDFromDocDB(endpoint.DocDbSettings),
		secretIDFromRedshift(endpoint.RedshiftSettings),
		secretIDFromSybase(endpoint.SybaseSettings),
		secretIDFromIBMDb2(endpoint.IBMDb2Settings),
		secretIDFromGcpMySQL(endpoint.GcpMySQLSettings),
	}
	for _, candidate := range candidates {
		if id := strings.TrimSpace(aws.ToString(candidate)); id != "" {
			return id
		}
	}
	return ""
}

func secretIDFromMySQL(s *awsdmstypes.MySQLSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromPostgreSQL(s *awsdmstypes.PostgreSQLSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromOracle(s *awsdmstypes.OracleSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromSQLServer(s *awsdmstypes.MicrosoftSQLServerSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromMongoDB(s *awsdmstypes.MongoDbSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromDocDB(s *awsdmstypes.DocDbSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromRedshift(s *awsdmstypes.RedshiftSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromSybase(s *awsdmstypes.SybaseSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromIBMDb2(s *awsdmstypes.IBMDb2Settings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}

func secretIDFromGcpMySQL(s *awsdmstypes.GcpMySQLSettings) *string {
	if s == nil {
		return nil
	}
	return s.SecretsManagerSecretId
}
