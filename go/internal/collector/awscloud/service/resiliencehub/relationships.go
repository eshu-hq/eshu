// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// appUsesPolicyRelationship records that an application is governed by a
// resiliency policy. AWS reports a policy ARN, which matches the resource_id the
// resiliency-policy node publishes. It returns nil when no policy is attached.
func appUsesPolicyRelationship(boundary awscloud.Boundary, app App) *awscloud.RelationshipObservation {
	policyARN := strings.TrimSpace(app.PolicyARN)
	if policyARN == "" {
		return nil
	}
	appID := appResourceID(app)
	if appID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResilienceHubAppUsesPolicy,
		SourceResourceID: appID,
		SourceARN:        strings.TrimSpace(app.ARN),
		TargetResourceID: policyARN,
		TargetARN:        policyARN,
		TargetType:       awscloud.ResourceTypeResilienceHubResiliencyPolicy,
		SourceRecordID:   appID + "->" + awscloud.RelationshipResilienceHubAppUsesPolicy + ":" + policyARN,
	}
}

// appProtectsResourceRelationship records that an application protects a physical
// AWS resource. It is emitted only when the Resilience Hub-reported type maps to
// an Eshu resource family the owning scanner keys by ARN and the reported
// identifier is ARN-shaped, so the edge always joins the owning node. It returns
// nil otherwise, skipping the edge instead of dangling it.
func appProtectsResourceRelationship(
	boundary awscloud.Boundary,
	app App,
	resource ProtectedResource,
) *awscloud.RelationshipObservation {
	targetType := protectedResourceTargetType(resource.ResilienceHubType)
	targetARN := strings.TrimSpace(resource.ARN)
	if targetType == "" || !isARN(targetARN) {
		return nil
	}
	appID := appResourceID(app)
	if appID == "" {
		return nil
	}
	attributes := map[string]any{
		"resource_type": strings.TrimSpace(resource.ResilienceHubType),
	}
	if logical := strings.TrimSpace(resource.LogicalResourceID); logical != "" {
		attributes["logical_resource_id"] = logical
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResilienceHubAppProtectsResource,
		SourceResourceID: appID,
		SourceARN:        strings.TrimSpace(app.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attributes,
		SourceRecordID:   appID + "->" + awscloud.RelationshipResilienceHubAppProtectsResource + ":" + targetARN,
	}
}

// componentInAppRelationship records an application component's membership in its
// parent application, keyed by the application ARN the application node
// publishes. It returns nil when either endpoint identity is missing.
func componentInAppRelationship(
	boundary awscloud.Boundary,
	app App,
	component AppComponent,
) *awscloud.RelationshipObservation {
	appID := appResourceID(app)
	componentID := componentResourceID(appID, component)
	if appID == "" || componentID == "" {
		return nil
	}
	targetARN := ""
	if isARN(appID) {
		targetARN = appID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResilienceHubComponentInApp,
		SourceResourceID: componentID,
		TargetResourceID: appID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeResilienceHubApp,
		SourceRecordID:   componentID + "->" + awscloud.RelationshipResilienceHubComponentInApp + ":" + appID,
	}
}

// inputSourceInAppRelationship records an input source's membership in its parent
// application, keyed by the application ARN the application node publishes. It
// returns nil when either endpoint identity is missing.
func inputSourceInAppRelationship(
	boundary awscloud.Boundary,
	app App,
	source InputSource,
) *awscloud.RelationshipObservation {
	appID := appResourceID(app)
	sourceID := inputSourceResourceID(appID, source)
	if appID == "" || sourceID == "" {
		return nil
	}
	sourceARN := ""
	if isARN(sourceID) {
		sourceARN = sourceID
	}
	targetARN := ""
	if isARN(appID) {
		targetARN = appID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResilienceHubInputSourceInApp,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: appID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeResilienceHubApp,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipResilienceHubInputSourceInApp + ":" + appID,
	}
}

// assessmentForAppRelationship records that an assessment was run for an
// application, keyed by the application ARN the application node publishes. AWS
// reports the application ARN on the assessment summary directly. It returns nil
// when either endpoint identity is missing.
func assessmentForAppRelationship(
	boundary awscloud.Boundary,
	assessment Assessment,
) *awscloud.RelationshipObservation {
	assessmentID := assessmentResourceID(assessment)
	appARN := strings.TrimSpace(assessment.AppARN)
	if assessmentID == "" || appARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipResilienceHubAssessmentForApp,
		SourceResourceID: assessmentID,
		SourceARN:        strings.TrimSpace(assessment.ARN),
		TargetResourceID: appARN,
		TargetARN:        appARN,
		TargetType:       awscloud.ResourceTypeResilienceHubApp,
		SourceRecordID:   assessmentID + "->" + awscloud.RelationshipResilienceHubAssessmentForApp + ":" + appARN,
	}
}
