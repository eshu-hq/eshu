// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"encoding/json"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsopensearchtypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	awsserverlesstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"

	opensearchservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/opensearch"
)

const (
	// batchGetCollectionLimit caps the ids per BatchGetCollection call. AWS
	// accepts up to 100 collection identifiers per batch.
	batchGetCollectionLimit = 100
	// batchGetVPCEndpointLimit caps the ids per BatchGetVpcEndpoint call. AWS
	// accepts up to 100 endpoint identifiers per batch.
	batchGetVPCEndpointLimit = 100
)

// mapDomain projects an OpenSearch DescribeDomains status into the scanner-owned
// metadata view. The AWS DomainStatus response never includes the master user
// password, and the adapter deliberately drops the domain endpoint, endpoints
// map, and access policy body; only IAM role ARNs referenced by the access
// policy are resolved for relationship evidence.
func mapDomain(raw awsopensearchtypes.DomainStatus, tags map[string]string) opensearchservice.Domain {
	domain := opensearchservice.Domain{
		ARN:           strings.TrimSpace(aws.ToString(raw.ARN)),
		ID:            strings.TrimSpace(aws.ToString(raw.DomainId)),
		Name:          strings.TrimSpace(aws.ToString(raw.DomainName)),
		EngineVersion: strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Tags:          tags,
	}
	if raw.Created != nil && !aws.ToBool(raw.Created) {
		domain.State = "Creating"
	} else if aws.ToBool(raw.Processing) {
		domain.State = "Processing"
	} else {
		domain.State = "Active"
	}
	if cfg := raw.ClusterConfig; cfg != nil {
		domain.InstanceType = string(cfg.InstanceType)
		domain.InstanceCount = aws.ToInt32(cfg.InstanceCount)
		domain.DedicatedMasterEnabled = aws.ToBool(cfg.DedicatedMasterEnabled)
		domain.DedicatedMasterType = string(cfg.DedicatedMasterType)
		domain.DedicatedMasterCount = aws.ToInt32(cfg.DedicatedMasterCount)
		domain.ZoneAwarenessEnabled = aws.ToBool(cfg.ZoneAwarenessEnabled)
	}
	if enc := raw.EncryptionAtRestOptions; enc != nil {
		domain.EncryptionAtRestEnabled = aws.ToBool(enc.Enabled)
		domain.KMSKeyID = strings.TrimSpace(aws.ToString(enc.KmsKeyId))
	}
	if n2n := raw.NodeToNodeEncryptionOptions; n2n != nil {
		domain.NodeToNodeEncryptionOn = aws.ToBool(n2n.Enabled)
	}
	if vpc := raw.VPCOptions; vpc != nil {
		domain.VPCID = strings.TrimSpace(aws.ToString(vpc.VPCId))
		domain.SubnetIDs = trimmedStrings(vpc.SubnetIds)
		domain.SecurityGroupIDs = trimmedStrings(vpc.SecurityGroupIds)
		domain.AvailabilityZones = trimmedStrings(vpc.AvailabilityZones)
	}
	if sec := raw.AdvancedSecurityOptions; sec != nil {
		domain.AdvancedSecurityEnabled = aws.ToBool(sec.Enabled)
		domain.InternalUserDBEnabled = aws.ToBool(sec.InternalUserDatabaseEnabled)
		if sec.SAMLOptions != nil {
			domain.SAMLEnabled = aws.ToBool(sec.SAMLOptions.Enabled)
		}
		if sec.IAMFederationOptions != nil {
			domain.IAMFederationEnabled = aws.ToBool(sec.IAMFederationOptions.Enabled)
		}
	}
	domain.MasterUserRoleARNs = accessPolicyRoleARNs(aws.ToString(raw.AccessPolicies))
	return domain
}

func mapPackage(raw awsopensearchtypes.PackageDetails) opensearchservice.Package {
	return opensearchservice.Package{
		ID:            strings.TrimSpace(aws.ToString(raw.PackageID)),
		Name:          strings.TrimSpace(aws.ToString(raw.PackageName)),
		Type:          string(raw.PackageType),
		Status:        string(raw.PackageStatus),
		Description:   strings.TrimSpace(aws.ToString(raw.PackageDescription)),
		EngineVersion: strings.TrimSpace(aws.ToString(raw.EngineVersion)),
		Owner:         strings.TrimSpace(aws.ToString(raw.PackageOwner)),
	}
}

func mapCollection(raw awsserverlesstypes.CollectionDetail) opensearchservice.Collection {
	return opensearchservice.Collection{
		ARN:                strings.TrimSpace(aws.ToString(raw.Arn)),
		ID:                 strings.TrimSpace(aws.ToString(raw.Id)),
		Name:               strings.TrimSpace(aws.ToString(raw.Name)),
		Type:               string(raw.Type),
		Status:             string(raw.Status),
		Description:        strings.TrimSpace(aws.ToString(raw.Description)),
		KMSKeyARN:          strings.TrimSpace(aws.ToString(raw.KmsKeyArn)),
		StandbyReplicas:    string(raw.StandbyReplicas),
		DeletionProtection: string(raw.DeletionProtection),
	}
}

func mapVPCEndpoint(raw awsserverlesstypes.VpcEndpointDetail) opensearchservice.VPCEndpoint {
	return opensearchservice.VPCEndpoint{
		ID:               strings.TrimSpace(aws.ToString(raw.Id)),
		Name:             strings.TrimSpace(aws.ToString(raw.Name)),
		Status:           string(raw.Status),
		VPCID:            strings.TrimSpace(aws.ToString(raw.VpcId)),
		SubnetIDs:        trimmedStrings(raw.SubnetIds),
		SecurityGroupIDs: trimmedStrings(raw.SecurityGroupIds),
	}
}

// accessPolicyRoleARNs extracts IAM role ARNs from a domain access policy
// without persisting the policy body itself. The access policy is an IAM
// resource policy whose Statement[].Principal.AWS entries reference principals;
// only role-shaped ARNs (arn:<partition>:iam::<account>:role/...) are returned
// so the scanner can record a domain-to-IAM-role relationship. The partition is
// never synthesized: a value is treated as a role ARN only when AWS reported it
// in that shape.
func accessPolicyRoleARNs(policy string) []string {
	policy = strings.TrimSpace(policy)
	if policy == "" {
		return nil
	}
	var doc struct {
		Statement []struct {
			Principal json.RawMessage `json:"Principal"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(policy), &doc); err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var roles []string
	for _, statement := range doc.Statement {
		for _, principal := range awsPrincipals(statement.Principal) {
			if !isRoleARN(principal) {
				continue
			}
			if _, ok := seen[principal]; ok {
				continue
			}
			seen[principal] = struct{}{}
			roles = append(roles, principal)
		}
	}
	return roles
}

// awsPrincipals normalizes the polymorphic Principal field. AWS allows
// "Principal": "*", a string ARN, {"AWS": "arn"}, or {"AWS": ["arn", ...]}.
func awsPrincipals(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []string{strings.TrimSpace(asString)}
	}
	var asObject struct {
		AWS json.RawMessage `json:"AWS"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return nil
	}
	return jsonStringOrList(asObject.AWS)
}

func jsonStringOrList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{strings.TrimSpace(single)}
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, value := range list {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

// isRoleARN reports whether value is an IAM role ARN. It matches the
// arn:<partition>:iam::<account>:role/ shape without assuming the aws partition
// so aws-cn and aws-us-gov role ARNs are recognized.
func isRoleARN(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "arn:") {
		return false
	}
	return strings.Contains(value, ":iam:") && strings.Contains(value, ":role/")
}

func mapTags(tags []awsopensearchtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func trimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 || len(values) == 0 {
		return nil
	}
	var chunks [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}
