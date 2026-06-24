// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCodeGuru identifies the regional Amazon CodeGuru metadata-only scan
	// slice that covers both CodeGuru Reviewer and CodeGuru Profiler. The scanner
	// reads control-plane metadata through ListRepositoryAssociations (Reviewer)
	// and ListProfilingGroups (Profiler) and never reads recommendation content,
	// code-review findings, profiling sample data, flame graphs, or agent
	// telemetry payloads, and never mutates any CodeGuru resource.
	ServiceCodeGuru = "codeguru"
)

const (
	// ResourceTypeCodeGuruRepositoryAssociation identifies a CodeGuru Reviewer
	// repository association metadata resource. The scanner emits association
	// identity (ARN, association id, name, owner, provider type, state) and the
	// configured-backing references (CodeStar connection ARN, S3 bucket name,
	// customer-managed KMS key id) only. It never emits code-review findings,
	// recommendation text, or any analyzed source content.
	ResourceTypeCodeGuruRepositoryAssociation = "aws_codeguru_repository_association"
	// ResourceTypeCodeGuruProfilingGroup identifies a CodeGuru Profiler profiling
	// group metadata resource. The scanner emits group identity (ARN, name,
	// compute platform, profiling-enabled posture, lifecycle timestamps) only. It
	// never emits profiling samples, aggregated profiles, flame graphs,
	// recommendation reports, or agent telemetry.
	ResourceTypeCodeGuruProfilingGroup = "aws_codeguru_profiling_group"
)

const (
	// RelationshipCodeGuruAssociationReviewsCodeCommitRepository records a
	// CodeGuru Reviewer repository association whose provider is AWS CodeCommit.
	// The target is the partition-aware CodeCommit repository ARN
	// (arn:<partition>:codecommit:<region>:<owner-account>:<name>), which matches
	// how the CodeCommit scanner publishes its repository resource_id, so the edge
	// joins the existing repository node. Associations for non-CodeCommit
	// providers (GitHub, Bitbucket, S3, GitHub Enterprise Server) record their
	// backing reference as a resource attribute and emit no edge, never a
	// dangling one.
	RelationshipCodeGuruAssociationReviewsCodeCommitRepository = "codeguru_association_reviews_codecommit_repository"
)
