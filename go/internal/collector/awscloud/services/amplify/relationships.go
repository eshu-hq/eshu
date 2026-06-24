// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amplify

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// repositorySourceTargetType labels an Amplify app's Git source-repository
// relationship target. The repository is an external (non-AWS-resource) endpoint,
// so it carries the shared git_repository join anchor without claiming a scanned
// AWS resource node, mirroring how the CodeBuild scanner labels its source
// repositories. The relguard allowlist documents this value.
const repositorySourceTargetType = "git_repository"

// appRelationships derives every relationship one Amplify app reports directly:
// the Git source repository, the IAM service/compute role, and the custom-domain
// edges to Route 53 hosted zones and CloudFront distributions. Every edge sources
// on the same id the app node publishes (appID) so the app's outgoing edges join
// the app node, and every edge names a non-empty target type so the graph join
// resolves the target node. The domain associations are passed in because they
// come from a separate API read.
func appRelationships(
	boundary awscloud.Boundary,
	app App,
	domains []DomainAssociation,
) []awscloud.RelationshipObservation {
	appID := appResourceID(boundary, app)
	appArnValue := firstNonEmpty(app.ARN, appARN(boundary, app.ID))
	if appID == "" {
		return nil
	}

	var observations []awscloud.RelationshipObservation

	if rel, ok := repositoryRelationship(boundary, app, appArnValue, appID); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, roleRelationships(boundary, app, appArnValue, appID)...)
	observations = append(observations, domainRelationships(boundary, domains, appArnValue, appID)...)

	return observations
}

// repositoryRelationship maps the app's Git repository into an app->repository
// edge. The repository URL is host/path only (sanitized upstream), so the join
// key never carries a userinfo token. CodeCommit, GitHub, GitLab, and Bitbucket
// all flow through the same external git_repository anchor.
func repositoryRelationship(
	boundary awscloud.Boundary,
	app App,
	appArnValue, appID string,
) (awscloud.RelationshipObservation, bool) {
	repo := strings.TrimSpace(app.RepositoryURL)
	if repo == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAmplifyAppDeploysFromRepository,
		SourceResourceID: appID,
		SourceARN:        appArnValue,
		TargetResourceID: repo,
		TargetType:       repositorySourceTargetType,
		Attributes: map[string]any{
			"repository_clone_method": strings.TrimSpace(app.RepositoryCloneMethod),
		},
		SourceRecordID: appID + "#repository#" + repo,
	}, true
}

// roleRelationships maps the app's IAM service role and SSR compute role into
// app->IAM-role edges. The IAM scanner publishes its role resource_id as the
// role ARN, so each edge targets the role ARN directly. Both roles are emitted
// when present because they are distinct grants.
func roleRelationships(
	boundary awscloud.Boundary,
	app App,
	appArnValue, appID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, role := range []struct {
		arn  string
		kind string
	}{
		{arn: app.ServiceRoleARN, kind: "service"},
		{arn: app.ComputeRoleARN, kind: "compute"},
	} {
		roleARN := strings.TrimSpace(role.arn)
		if roleARN == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAmplifyAppUsesIAMRole,
			SourceResourceID: appID,
			SourceARN:        appArnValue,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			Attributes:       map[string]any{"role_kind": role.kind},
			SourceRecordID:   appID + "#role#" + role.kind + "#" + roleARN,
		})
	}
	return observations
}

// domainRelationships maps each custom-domain association into the resolvable
// custom-domain edges. The Route 53 edge keys on the normalized domain root,
// matching the hosted-zone scanner's normalized_name correlation anchor, and the
// CloudFront edge keys on the *.cloudfront.net distribution domain extracted from
// a subdomain DNS record, matching the CloudFront scanner's domain-name anchor.
// Neither edge carries a target ARN because Amplify reports only a domain name,
// not an ARN; a fabricated ARN would dangle in any partition.
func domainRelationships(
	boundary awscloud.Boundary,
	domains []DomainAssociation,
	appArnValue, appID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	seenCloudFront := map[string]struct{}{}
	for _, domain := range domains {
		domainName := normalizedDomain(domain.DomainName)
		if domainName != "" {
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipAmplifyAppServesCustomDomainViaHostedZone,
				SourceResourceID: appID,
				SourceARN:        appArnValue,
				TargetResourceID: domainName,
				TargetType:       awscloud.ResourceTypeRoute53HostedZone,
				Attributes: map[string]any{
					"domain_name":   domainName,
					"domain_status": strings.TrimSpace(domain.Status),
				},
				SourceRecordID: appID + "#domain-zone#" + domainName,
			})
		}
		for _, sub := range domain.SubDomains {
			cf := cloudFrontDomainFromDNSRecord(sub.DNSRecord)
			if cf == "" {
				continue
			}
			if _, ok := seenCloudFront[cf]; ok {
				continue
			}
			seenCloudFront[cf] = struct{}{}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipAmplifyAppServesCustomDomainViaCloudFront,
				SourceResourceID: appID,
				SourceARN:        appArnValue,
				TargetResourceID: cf,
				TargetType:       awscloud.ResourceTypeCloudFrontDistribution,
				Attributes: map[string]any{
					"domain_name":  domainName,
					"subdomain":    strings.TrimSpace(sub.Prefix),
					"distribution": cf,
				},
				SourceRecordID: appID + "#domain-cloudfront#" + cf,
			})
		}
	}
	return observations
}

// branchAppRelationship maps a branch into a branch->app edge. The target is the
// app node's published resource_id (the app ARN), so the edge joins the app node
// the app scan emits. The branch's own resource_id is the source.
func branchAppRelationship(
	boundary awscloud.Boundary,
	branch Branch,
	appResourceIDValue string,
) (awscloud.RelationshipObservation, bool) {
	branchID := branchResourceID(boundary, branch)
	appResourceIDValue = strings.TrimSpace(appResourceIDValue)
	if branchID == "" || appResourceIDValue == "" {
		return awscloud.RelationshipObservation{}, false
	}
	appArnValue := ""
	if strings.HasPrefix(appResourceIDValue, "arn:") {
		appArnValue = appResourceIDValue
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAmplifyBranchBelongsToApp,
		SourceResourceID: branchID,
		SourceARN:        strings.TrimSpace(branch.ARN),
		TargetResourceID: appResourceIDValue,
		TargetARN:        appArnValue,
		TargetType:       awscloud.ResourceTypeAmplifyApp,
		Attributes:       map[string]any{"branch_name": strings.TrimSpace(branch.Name)},
		SourceRecordID:   branchID + "#belongs-to#" + appResourceIDValue,
	}, true
}
