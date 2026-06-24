// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewPrincipalEnvelope builds the durable aws_iam_principal source fact for one
// AWS IAM principal identity.
func NewPrincipalEnvelope(observation PrincipalObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	principalARN := strings.TrimSpace(observation.PrincipalARN)
	principalType := strings.TrimSpace(observation.PrincipalType)
	if principalARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam principal observation requires principal_arn")
	}
	if principalType == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam principal observation requires principal_type")
	}
	stableKey := facts.StableID(facts.AWSIAMPrincipalFactKind, map[string]any{
		"account_id":     observation.Context.AccountID,
		"principal_arn":  principalARN,
		"principal_type": principalType,
		"region":         observation.Context.Region,
	})
	payload := commonPayload(observation.Context)
	payload["principal_arn"] = principalARN
	payload["principal_id"] = principalARN
	payload["principal_type"] = principalType
	payload["name"] = strings.TrimSpace(observation.Name)
	payload["path"] = strings.TrimSpace(observation.Path)
	payload["url_fingerprint"] = strings.TrimSpace(observation.URLFingerprint)
	payload["url_present"] = strings.TrimSpace(observation.URLFingerprint) != ""
	payload["client_id_count"] = observation.ClientIDCount
	payload["thumbprint_count"] = observation.ThumbprintCount
	payload["correlation_hints"] = normalizePatternList(observation.CorrelationHints)
	return newEnvelope(
		observation.Context,
		facts.AWSIAMPrincipalFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, principalARN),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewTrustPolicyEnvelope builds a durable aws_iam_trust_policy source fact for
// one normalized IAM role trust policy statement.
func NewTrustPolicyEnvelope(observation TrustPolicyObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	roleARN := strings.TrimSpace(observation.RoleARN)
	if roleARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam trust policy observation requires role_arn")
	}
	effect := normalizeEffect(observation.Effect)
	if effect == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam trust policy observation requires effect")
	}
	actions := normalizeActionList(observation.Actions)
	conditionKeys := normalizeKeyList(observation.ConditionKeys)
	conditionOperators := normalizeKeyList(observation.ConditionOperators)
	assumePrincipals := normalizePatternList(observation.AssumePrincipals)
	webIdentitySubjects := normalizePatternList(observation.WebIdentitySubjectFingerprints)
	statementSID := strings.TrimSpace(observation.StatementSID)
	stableIdentity := map[string]any{
		"account_id":            observation.Context.AccountID,
		"actions":               strings.Join(actions, ","),
		"assume_principals":     strings.Join(assumePrincipals, ","),
		"effect":                effect,
		"region":                observation.Context.Region,
		"role_arn":              roleARN,
		"statement_sid":         statementSID,
		"web_identity_subjects": strings.Join(webIdentitySubjects, ","),
	}
	addConditionSummaryIdentity(stableIdentity, conditionKeys, conditionOperators)
	stableKey := facts.StableID(facts.AWSIAMTrustPolicyFactKind, stableIdentity)
	payload := commonPayload(observation.Context)
	payload["role_arn"] = roleARN
	payload["statement_sid"] = statementSID
	payload["policy_source"] = PolicySourceTrust
	payload["effect"] = effect
	payload["actions"] = actions
	payload["condition_keys"] = conditionKeys
	payload["condition_operators"] = conditionOperators
	payload["condition_operator_count"] = len(conditionOperators)
	payload["assume_principals"] = assumePrincipals
	payload["has_conditions"] = len(conditionKeys) > 0 || len(conditionOperators) > 0
	payload["web_identity_subject_fingerprints"] = webIdentitySubjects
	payload["web_identity_subject_wildcard"] = observation.WebIdentitySubjectWildcard
	return newEnvelope(
		observation.Context,
		facts.AWSIAMTrustPolicyFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, trustPolicySourceID(roleARN, statementSID, effect, actions)),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewPermissionPolicyEnvelope builds the durable aws_iam_permission_policy
// source fact for one normalized IAM identity policy statement.
func NewPermissionPolicyEnvelope(observation PermissionPolicyObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	principalARN := strings.TrimSpace(observation.PrincipalARN)
	if principalARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam permission policy observation requires principal_arn")
	}
	effect := normalizeEffect(observation.Effect)
	if effect == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam permission policy observation requires effect")
	}
	policySource := strings.TrimSpace(observation.PolicySource)
	if policySource == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam permission policy observation requires policy_source")
	}

	actions := normalizeActionList(observation.Actions)
	notActions := normalizeActionList(observation.NotActions)
	resources := normalizePatternList(observation.Resources)
	notResources := normalizePatternList(observation.NotResources)
	conditionKeys := normalizeKeyList(observation.ConditionKeys)
	conditionOperators := normalizeKeyList(observation.ConditionOperators)
	policyARN := strings.TrimSpace(observation.PolicyARN)
	policyName := strings.TrimSpace(observation.PolicyName)
	statementSID := strings.TrimSpace(observation.StatementSID)

	stableIdentity := map[string]any{
		"account_id":    observation.Context.AccountID,
		"actions":       strings.Join(actions, ","),
		"effect":        effect,
		"not_actions":   strings.Join(notActions, ","),
		"not_resources": strings.Join(notResources, ","),
		"policy_arn":    policyARN,
		"policy_name":   policyName,
		"policy_source": policySource,
		"principal_arn": principalARN,
		"region":        observation.Context.Region,
		"resources":     strings.Join(resources, ","),
		"statement_sid": statementSID,
	}
	addConditionSummaryIdentity(stableIdentity, conditionKeys, conditionOperators)
	stableKey := facts.StableID(facts.AWSIAMPermissionPolicyFactKind, stableIdentity)
	payload := commonPayload(observation.Context)
	payload["principal_arn"] = principalARN
	payload["principal_type"] = strings.TrimSpace(observation.PrincipalType)
	payload["policy_source"] = policySource
	payload["policy_arn"] = policyARN
	payload["policy_name"] = policyName
	payload["statement_sid"] = statementSID
	payload["effect"] = effect
	payload["actions"] = actions
	payload["not_actions"] = notActions
	payload["resources"] = resources
	payload["not_resources"] = notResources
	payload["condition_keys"] = conditionKeys
	payload["condition_operators"] = conditionOperators
	payload["condition_operator_count"] = len(conditionOperators)
	payload["has_conditions"] = len(conditionKeys) > 0 || len(conditionOperators) > 0
	payload["is_wildcard_action"] = containsValue(actions, wildcardAction)
	payload["is_wildcard_resource"] = containsValue(resources, wildcardAction)
	return newEnvelope(
		observation.Context,
		facts.AWSIAMPermissionPolicyFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, permissionPolicySourceID(principalARN, policySource, policyARN, policyName, statementSID, effect, actions)),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewPolicyAttachmentEnvelope builds the durable aws_iam_policy_attachment
// source fact for one managed policy attachment to a principal.
func NewPolicyAttachmentEnvelope(observation PolicyAttachmentObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	principalARN := strings.TrimSpace(observation.PrincipalARN)
	policyARN := strings.TrimSpace(observation.PolicyARN)
	if principalARN == "" || policyARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam policy attachment observation requires principal_arn and policy_arn")
	}
	policySource := firstNonBlank(observation.PolicySource, PolicySourceAttachedManaged)
	stableKey := facts.StableID(facts.AWSIAMPolicyAttachmentFactKind, map[string]any{
		"account_id":    observation.Context.AccountID,
		"policy_arn":    policyARN,
		"policy_source": policySource,
		"principal_arn": principalARN,
		"region":        observation.Context.Region,
	})
	payload := commonPayload(observation.Context)
	payload["principal_arn"] = principalARN
	payload["principal_type"] = strings.TrimSpace(observation.PrincipalType)
	payload["policy_arn"] = policyARN
	payload["policy_name"] = strings.TrimSpace(observation.PolicyName)
	payload["policy_source"] = policySource
	return newEnvelope(
		observation.Context,
		facts.AWSIAMPolicyAttachmentFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, principalARN+"#attachment#"+policyARN),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewPermissionBoundaryEnvelope builds the durable
// aws_iam_permission_boundary source fact for one boundary attached to a
// principal.
func NewPermissionBoundaryEnvelope(observation PermissionBoundaryObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	principalARN := strings.TrimSpace(observation.PrincipalARN)
	boundaryPolicyARN := strings.TrimSpace(observation.BoundaryPolicyARN)
	if principalARN == "" || boundaryPolicyARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam permission boundary observation requires principal_arn and boundary_policy_arn")
	}
	stableKey := facts.StableID(facts.AWSIAMPermissionBoundaryFactKind, map[string]any{
		"account_id":          observation.Context.AccountID,
		"boundary_policy_arn": boundaryPolicyARN,
		"principal_arn":       principalARN,
		"region":              observation.Context.Region,
	})
	payload := commonPayload(observation.Context)
	payload["principal_arn"] = principalARN
	payload["principal_type"] = strings.TrimSpace(observation.PrincipalType)
	payload["boundary_policy_arn"] = boundaryPolicyARN
	payload["boundary_type"] = strings.TrimSpace(observation.BoundaryType)
	return newEnvelope(
		observation.Context,
		facts.AWSIAMPermissionBoundaryFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, principalARN+"#boundary#"+boundaryPolicyARN),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewInstanceProfileEnvelope builds the durable aws_iam_instance_profile source
// fact for one IAM instance profile.
func NewInstanceProfileEnvelope(observation InstanceProfileObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	profileARN := strings.TrimSpace(observation.ProfileARN)
	if profileARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam instance profile observation requires profile_arn")
	}
	roleARNs := normalizePatternList(observation.RoleARNs)
	stableKey := facts.StableID(facts.AWSIAMInstanceProfileFactKind, map[string]any{
		"account_id":  observation.Context.AccountID,
		"profile_arn": profileARN,
		"region":      observation.Context.Region,
	})
	payload := commonPayload(observation.Context)
	payload["profile_arn"] = profileARN
	payload["name"] = strings.TrimSpace(observation.Name)
	payload["path"] = strings.TrimSpace(observation.Path)
	payload["role_arns"] = roleARNs
	payload["role_count"] = len(roleARNs)
	return newEnvelope(
		observation.Context,
		facts.AWSIAMInstanceProfileFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, profileARN),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewAccessAnalyzerFindingEnvelope builds an optional
// aws_iam_access_analyzer_finding source fact without embedding finding bodies.
func NewAccessAnalyzerFindingEnvelope(observation AccessAnalyzerFindingObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	findingID := strings.TrimSpace(observation.FindingID)
	resourceARN := strings.TrimSpace(observation.ResourceARN)
	if findingID == "" && resourceARN == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam access analyzer finding observation requires finding_id or resource_arn")
	}
	conditionKeys := normalizeKeyList(observation.ConditionKeys)
	stableKey := facts.StableID(facts.AWSIAMAccessAnalyzerFindingFactKind, map[string]any{
		"account_id":   observation.Context.AccountID,
		"analyzer_arn": strings.TrimSpace(observation.AnalyzerARN),
		"finding_id":   findingID,
		"region":       observation.Context.Region,
		"resource_arn": resourceARN,
	})
	payload := commonPayload(observation.Context)
	payload["finding_id"] = findingID
	payload["analyzer_arn"] = strings.TrimSpace(observation.AnalyzerARN)
	payload["resource_arn"] = resourceARN
	payload["resource_type"] = strings.TrimSpace(observation.ResourceType)
	payload["status"] = strings.TrimSpace(observation.Status)
	payload["finding_type"] = strings.TrimSpace(observation.FindingType)
	payload["condition_keys"] = conditionKeys
	return newEnvelope(
		observation.Context,
		facts.AWSIAMAccessAnalyzerFindingFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, firstNonBlank(findingID, resourceARN)),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewCoverageWarningEnvelope builds the durable
// secrets_iam_coverage_warning source fact for explicit partial, hidden,
// unsupported, rate-limited, or stale source coverage.
func NewCoverageWarningEnvelope(observation CoverageWarningObservation) (facts.Envelope, error) {
	if err := validateContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	sourceState := strings.TrimSpace(observation.SourceState)
	if warningKind == "" || sourceState == "" {
		return facts.Envelope{}, fmt.Errorf("secrets iam coverage warning observation requires warning_kind and source_state")
	}
	stableKey := facts.StableID(facts.SecretsIAMCoverageWarningFactKind, map[string]any{
		"account_id":   observation.Context.AccountID,
		"generation":   observation.Context.GenerationID,
		"region":       observation.Context.Region,
		"source_state": sourceState,
		"warning_kind": warningKind,
	})
	payload := commonPayload(observation.Context)
	payload["warning_kind"] = warningKind
	payload["source_state"] = sourceState
	payload["error_class"] = strings.TrimSpace(observation.ErrorClass)
	payload["message"] = strings.TrimSpace(observation.Message)
	payload["attributes"] = cloneAnyMap(observation.Attributes)
	return newEnvelope(
		observation.Context,
		facts.SecretsIAMCoverageWarningFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, warningKind),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

func newEnvelope(
	ctx EnvelopeContext,
	factKind string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	payload map[string]any,
) facts.Envelope {
	return facts.Envelope{
		FactID:           secretsIAMFactID(factKind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          strings.TrimSpace(ctx.ScopeID),
		GenerationID:     strings.TrimSpace(ctx.GenerationID),
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.SecretsIAMSchemaVersionV1,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        strings.TrimSpace(ctx.ScopeID),
			GenerationID:   strings.TrimSpace(ctx.GenerationID),
			FactKey:        stableKey,
			SourceURI:      strings.TrimSpace(sourceURI),
			SourceRecordID: strings.TrimSpace(sourceRecordID),
		},
	}
}

func commonPayload(ctx EnvelopeContext) map[string]any {
	return map[string]any{
		"account_id":               strings.TrimSpace(ctx.AccountID),
		"region":                   strings.TrimSpace(ctx.Region),
		"provider":                 ProviderAWSIAM,
		"collector_instance_id":    strings.TrimSpace(ctx.CollectorInstanceID),
		"redaction_policy_version": RedactionPolicyVersion,
	}
}

func validateContext(ctx EnvelopeContext) error {
	switch {
	case strings.TrimSpace(ctx.AccountID) == "":
		return fmt.Errorf("secrets iam observation requires account_id")
	case strings.TrimSpace(ctx.Region) == "":
		return fmt.Errorf("secrets iam observation requires region")
	case strings.TrimSpace(ctx.ScopeID) == "":
		return fmt.Errorf("secrets iam observation requires scope_id")
	case strings.TrimSpace(ctx.GenerationID) == "":
		return fmt.Errorf("secrets iam observation requires generation_id")
	case strings.TrimSpace(ctx.CollectorInstanceID) == "":
		return fmt.Errorf("secrets iam observation requires collector_instance_id")
	case ctx.FencingToken <= 0:
		return fmt.Errorf("secrets iam observation fencing_token must be positive")
	default:
		return nil
	}
}

func secretsIAMFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("SecretsIAMFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedObservedAt(input time.Time) time.Time {
	if input.IsZero() {
		return time.Now().UTC()
	}
	return input.UTC()
}

// WebIdentitySubjectFingerprint returns the redaction-safe join fingerprint for
// an IAM web-identity subject such as system:serviceaccount:<ns>:<name>.
func WebIdentitySubjectFingerprint(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ""
	}
	return "sha256:" + facts.StableID("SecretsIAMWebIdentitySubject", map[string]any{
		"subject": subject,
	})
}
