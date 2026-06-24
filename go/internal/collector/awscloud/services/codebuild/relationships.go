// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codebuild

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// repositorySourceTargetType labels a Git source-provider relationship target.
// The repository is an external (non-AWS-resource) endpoint, so it carries a
// stable non-empty target type without claiming a scanned AWS resource node,
// mirroring how the Lambda scanner labels container-image targets.
const repositorySourceTargetType = "git_repository"

// projectSourceTypeS3 is the CodeBuild source type that stores input in S3.
const projectSourceTypeS3 = "S3"

// environmentVariableTypeSecretsManager and environmentVariableTypeParameterStore
// are the CodeBuild environment-variable types whose Value names a Secrets
// Manager secret or an SSM Parameter Store parameter rather than a literal
// value.
const (
	environmentVariableTypeSecretsManager = "SECRETS_MANAGER"
	environmentVariableTypeParameterStore = "PARAMETER_STORE"
)

// projectRelationships derives the relationship observations CodeBuild reports
// directly for one build project: service role, VPC/subnet/security-group,
// KMS key, S3 or Git source, S3 artifacts, and Secrets Manager / SSM Parameter
// Store environment-variable references. Each edge names a concrete target with
// a non-empty target type so the graph join resolves the target node.
func projectRelationships(
	boundary awscloud.Boundary,
	project Project,
) []awscloud.RelationshipObservation {
	projectARN := strings.TrimSpace(project.ARN)
	projectID := firstNonEmpty(projectARN, project.Name)
	if projectID == "" {
		return nil
	}

	var observations []awscloud.RelationshipObservation

	if rel, ok := serviceRoleRelationship(boundary, project, projectARN, projectID); ok {
		observations = append(observations, rel)
	}
	if rel, ok := kmsKeyRelationship(boundary, project, projectARN, projectID); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, vpcRelationships(boundary, project, projectARN, projectID)...)
	observations = append(observations, sourceRelationships(boundary, project, projectARN, projectID)...)
	observations = append(observations, artifactRelationships(boundary, project, projectARN, projectID)...)
	observations = append(observations, environmentReferenceRelationships(boundary, project, projectARN, projectID)...)

	return observations
}

func serviceRoleRelationship(
	boundary awscloud.Boundary,
	project Project,
	projectARN, projectID string,
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(project.ServiceRoleARN)
	if roleARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeBuildProjectUsesIAMRole,
		SourceResourceID: projectID,
		SourceARN:        projectARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   projectID + "#service-role#" + roleARN,
	}, true
}

func kmsKeyRelationship(
	boundary awscloud.Boundary,
	project Project,
	projectARN, projectID string,
) (awscloud.RelationshipObservation, bool) {
	keyID := strings.TrimSpace(project.EncryptionKeyID)
	if keyID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	// The KMS scanner emits its key resource_id as firstNonEmpty(keyID, keyARN),
	// so a key ARN joins the key node directly and a bare key id or alias joins
	// when the project reports that form. CodeBuild reports either an ARN or an
	// alias/ prefixed value; both are passed through unchanged.
	targetARN := ""
	if strings.HasPrefix(keyID, "arn:") {
		targetARN = keyID
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeBuildProjectUsesKMSKey,
		SourceResourceID: projectID,
		SourceARN:        projectARN,
		TargetResourceID: keyID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   projectID + "#kms#" + keyID,
	}, true
}

func vpcRelationships(
	boundary awscloud.Boundary,
	project Project,
	projectARN, projectID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	vpcID := strings.TrimSpace(project.VPCConfig.VPCID)
	if vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeBuildProjectUsesVPC,
			SourceResourceID: projectID,
			SourceARN:        projectARN,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   projectID + "#vpc#" + vpcID,
		})
	}
	for _, subnetID := range project.VPCConfig.SubnetIDs {
		subnetID = strings.TrimSpace(subnetID)
		if subnetID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeBuildProjectUsesSubnet,
			SourceResourceID: projectID,
			SourceARN:        projectARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   projectID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range project.VPCConfig.SecurityGroupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeBuildProjectUsesSecurityGroup,
			SourceResourceID: projectID,
			SourceARN:        projectARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   projectID + "#security-group#" + groupID,
		})
	}
	return observations
}

func sourceRelationships(
	boundary awscloud.Boundary,
	project Project,
	projectARN, projectID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, source := range append([]ProjectSource{project.Source}, project.SecondarySources...) {
		if rel, ok := sourceRelationship(boundary, source, projectARN, projectID); ok {
			observations = append(observations, rel)
		}
	}
	return observations
}

// sourceRelationship maps one CodeBuild source into a relationship. An S3
// source joins the S3 bucket node by bucket ARN. A Git provider source
// (GitHub, GitHub Enterprise, CodeCommit, Bitbucket, GitLab) joins an external
// repository target labeled with a stable non-AWS-resource target type. A
// CODEPIPELINE or NO_SOURCE source names no concrete resource and is skipped.
func sourceRelationship(
	boundary awscloud.Boundary,
	source ProjectSource,
	projectARN, projectID string,
) (awscloud.RelationshipObservation, bool) {
	sourceType := strings.TrimSpace(source.Type)
	location := strings.TrimSpace(source.Location)
	if location == "" {
		return awscloud.RelationshipObservation{}, false
	}
	if strings.EqualFold(sourceType, projectSourceTypeS3) {
		bucketARN := s3BucketARNFromLocation(boundary, location)
		return awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeBuildProjectSourcedFromS3,
			SourceResourceID: projectID,
			SourceARN:        projectARN,
			TargetResourceID: bucketARN,
			TargetARN:        bucketARN,
			TargetType:       awscloud.ResourceTypeS3Bucket,
			Attributes: map[string]any{
				"source_type":       sourceType,
				"source_location":   location,
				"source_identifier": strings.TrimSpace(source.SourceIdentifier),
			},
			SourceRecordID: projectID + "#source-s3#" + bucketARN,
		}, true
	}
	switch strings.ToUpper(sourceType) {
	case "CODEPIPELINE", "NO_SOURCE", "":
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeBuildProjectSourcedFromRepository,
		SourceResourceID: projectID,
		SourceARN:        projectARN,
		TargetResourceID: location,
		TargetType:       repositorySourceTargetType,
		Attributes: map[string]any{
			"source_type":       sourceType,
			"source_identifier": strings.TrimSpace(source.SourceIdentifier),
		},
		SourceRecordID: projectID + "#source-repo#" + location,
	}, true
}

func artifactRelationships(
	boundary awscloud.Boundary,
	project Project,
	projectARN, projectID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, artifact := range append([]ProjectArtifacts{project.Artifacts}, project.SecondaryArtifacts...) {
		if rel, ok := artifactRelationship(boundary, artifact, projectARN, projectID); ok {
			observations = append(observations, rel)
		}
	}
	return observations
}

// artifactRelationship maps one CodeBuild artifact configuration into an S3
// relationship. Only S3 artifacts name a concrete bucket; NO_ARTIFACTS and
// CODEPIPELINE artifact types are skipped.
func artifactRelationship(
	boundary awscloud.Boundary,
	artifact ProjectArtifacts,
	projectARN, projectID string,
) (awscloud.RelationshipObservation, bool) {
	if !strings.EqualFold(strings.TrimSpace(artifact.Type), projectSourceTypeS3) {
		return awscloud.RelationshipObservation{}, false
	}
	location := strings.TrimSpace(artifact.Location)
	if location == "" {
		return awscloud.RelationshipObservation{}, false
	}
	bucketARN := s3BucketARNFromLocation(boundary, location)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeBuildProjectArtifactsToS3,
		SourceResourceID: projectID,
		SourceARN:        projectARN,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes: map[string]any{
			"artifact_location":   location,
			"artifact_identifier": strings.TrimSpace(artifact.ArtifactIdentifier),
		},
		SourceRecordID: projectID + "#artifact-s3#" + bucketARN,
	}, true
}

// environmentReferenceRelationships derives Secrets Manager and SSM Parameter
// Store relationships from environment variables. SECRETS_MANAGER variables
// reference a secret ARN or name; PARAMETER_STORE variables reference a
// parameter name. Both target the resource_id the owning scanner emits, which
// is firstNonEmpty(ARN, name). PLAINTEXT variables name no resource and are
// excluded here.
func environmentReferenceRelationships(
	boundary awscloud.Boundary,
	project Project,
	projectARN, projectID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, variable := range project.Environment.EnvironmentVariables {
		reference := strings.TrimSpace(variable.Reference)
		if reference == "" {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(variable.Type)) {
		case environmentVariableTypeSecretsManager:
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipCodeBuildProjectReferencesSecret,
				SourceResourceID: projectID,
				SourceARN:        projectARN,
				TargetResourceID: reference,
				TargetARN:        secretARNOrEmpty(reference),
				TargetType:       awscloud.ResourceTypeSecretsManagerSecret,
				Attributes:       map[string]any{"environment_variable": strings.TrimSpace(variable.Name)},
				SourceRecordID:   projectID + "#secret#" + reference,
			})
		case environmentVariableTypeParameterStore:
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipCodeBuildProjectReferencesSSMParameter,
				SourceResourceID: projectID,
				SourceARN:        projectARN,
				TargetResourceID: reference,
				TargetARN:        ssmParameterARNOrEmpty(reference),
				TargetType:       awscloud.ResourceTypeSSMParameter,
				Attributes:       map[string]any{"environment_variable": strings.TrimSpace(variable.Name)},
				SourceRecordID:   projectID + "#ssm#" + reference,
			})
		}
	}
	return observations
}

// s3BucketARNFromLocation derives the S3 bucket ARN from a CodeBuild S3 source
// or artifact location of the form "bucket-name/path/prefix". It matches the S3
// scanner identity, which emits the bucket ARN as its resource_id, so the edge
// joins the bucket node. When the location is a bare bucket/path, the partition
// is derived from the scan boundary's region; when it is already an S3 ARN, the
// source ARN's partition is preserved. A hardcoded commercial partition would
// dangle the project->S3 edge in GovCloud and China.
func s3BucketARNFromLocation(boundary awscloud.Boundary, location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	if strings.HasPrefix(location, "arn:") {
		// Already an ARN; reduce to the bucket ARN to match the S3 scanner.
		return bucketARNFromObjectARN(location)
	}
	bucket := location
	if slash := strings.IndexByte(location, '/'); slash >= 0 {
		bucket = location[:slash]
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":s3:::" + bucket
}

// bucketARNFromObjectARN trims an S3 object ARN down to the bucket ARN so the
// edge joins the S3 bucket node rather than a non-existent object node. It
// preserves the source ARN's partition (aws / aws-cn / aws-us-gov) by matching
// the partition-agnostic `:s3:::` segment rather than only the commercial
// `arn:aws:s3:::` prefix, so a GovCloud or China object ARN still reduces to the
// correct bucket node instead of being passed through whole.
func bucketARNFromObjectARN(arn string) string {
	const marker = ":s3:::"
	trimmed := strings.TrimSpace(arn)
	if !strings.HasPrefix(trimmed, "arn:") {
		return trimmed
	}
	idx := strings.Index(trimmed, marker)
	if idx < 0 {
		return trimmed
	}
	prefix := trimmed[:idx+len(marker)]
	rest := trimmed[idx+len(marker):]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		rest = rest[:slash]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return trimmed
	}
	return prefix + rest
}

// secretARNOrEmpty returns the reference as a target ARN only when CodeBuild
// reported a full Secrets Manager ARN. A bare secret name is still a valid join
// key against the Secrets Manager scanner resource_id, but it is not an ARN.
func secretARNOrEmpty(reference string) string {
	if strings.HasPrefix(reference, "arn:") {
		return reference
	}
	return ""
}

// ssmParameterARNOrEmpty returns the reference as a target ARN only when
// CodeBuild reported a full SSM parameter ARN. A bare parameter name is still a
// valid join key against the SSM scanner resource_id, but it is not an ARN.
func ssmParameterARNOrEmpty(reference string) string {
	if strings.HasPrefix(reference, "arn:") {
		return reference
	}
	return ""
}
