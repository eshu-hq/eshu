// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCodeCommit identifies the regional AWS CodeCommit metadata scan
	// slice. The scanner is metadata-only: it never reads commits, refs,
	// blobs, file contents, pull-request bodies, or comment text, and never
	// mutates any CodeCommit resource.
	ServiceCodeCommit = "codecommit"
)

const (
	// ResourceTypeCodeCommitRepository identifies an AWS CodeCommit repository
	// metadata resource. The scanner emits repository identity (name, ARN,
	// repository id), the default branch name, clone-URL host evidence, the
	// encryption KMS key id, and creation/last-modified timestamps. It never
	// emits commit, ref, blob, or file-content evidence. The repository
	// publishes correlation anchors (repository name and clone URLs) so a
	// CodeBuild project, CodePipeline source action, or Amplify app whose Git
	// source points at the repository joins this resource as the code-to-cloud
	// anchor.
	ResourceTypeCodeCommitRepository = "aws_codecommit_repository"
)

const (
	// RelationshipCodeCommitRepositoryEncryptedWithKMSKey records the KMS key a
	// CodeCommit repository is encrypted with. The target is the KMS key the
	// repository reports (a bare key id or a key ARN), typed aws_kms_key.
	RelationshipCodeCommitRepositoryEncryptedWithKMSKey = "codecommit_repository_encrypted_with_kms_key"
	// RelationshipCodeCommitRepositoryTriggersSNSTopic records an SNS topic wired
	// to a CodeCommit repository trigger. The target is the trigger destination
	// SNS topic ARN, typed aws_sns_topic. Only SNS-topic destinations produce an
	// edge; non-SNS trigger destinations (for example Lambda) are recorded as
	// resource attributes, not promoted to this relationship.
	RelationshipCodeCommitRepositoryTriggersSNSTopic = "codecommit_repository_triggers_sns_topic"
)
