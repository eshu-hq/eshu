package lambda

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const containerImageTargetType = "container_image"

// Scanner emits Lambda function, alias, event-source mapping, and relationship
// facts for one claimed account and region.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func cloneFloatMap(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]float64, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	return output
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

func vpcConfigMap(config VPCConfig) map[string]any {
	if config.VPCID == "" && len(config.SubnetIDs) == 0 && len(config.SecurityGroupIDs) == 0 {
		return nil
	}
	return map[string]any{
		"ipv6_allowed_for_dual_stack": config.IPv6AllowedForDS,
		"security_group_ids":          cloneStrings(config.SecurityGroupIDs),
		"subnet_ids":                  cloneStrings(config.SubnetIDs),
		"vpc_id":                      strings.TrimSpace(config.VPCID),
	}
}

func loggingConfigMap(config LoggingConfig) map[string]any {
	if config.LogGroup == "" && config.LogFormat == "" &&
		config.ApplicationLogLevel == "" && config.SystemLogLevel == "" {
		return nil
	}
	return map[string]any{
		"application_log_level": strings.TrimSpace(config.ApplicationLogLevel),
		"log_format":            strings.TrimSpace(config.LogFormat),
		"log_group":             strings.TrimSpace(config.LogGroup),
		"system_log_level":      strings.TrimSpace(config.SystemLogLevel),
	}
}

// Scan observes Lambda resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("lambda scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("lambda scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceLambda
	case awscloud.ServiceLambda:
	default:
		return nil, fmt.Errorf("lambda scanner received service_kind %q", boundary.ServiceKind)
	}

	functions, err := s.Client.ListFunctions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lambda functions: %w", err)
	}
	var envelopes []facts.Envelope
	for _, function := range functions {
		functionEnvelopes, err := s.functionEnvelopes(ctx, boundary, function)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, functionEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) functionEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	function Function,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(s.functionObservation(boundary, function))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range functionRelationships(boundary, function) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}

	aliases, err := s.Client.ListAliases(ctx, function)
	if err != nil {
		return nil, fmt.Errorf("list Lambda aliases for function %q: %w", function.Name, err)
	}
	for _, alias := range aliases {
		aliasEnvelopes, err := aliasEnvelopes(boundary, function, alias)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, aliasEnvelopes...)
	}

	mappings, err := s.Client.ListEventSourceMappings(ctx, function)
	if err != nil {
		return nil, fmt.Errorf("list Lambda event source mappings for function %q: %w", function.Name, err)
	}
	for _, mapping := range mappings {
		mappingEnvelopes, err := eventSourceMappingEnvelopes(boundary, function, mapping)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, mappingEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) functionObservation(
	boundary awscloud.Boundary,
	function Function,
) awscloud.ResourceObservation {
	functionARN := strings.TrimSpace(function.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          functionARN,
		ResourceID:   firstNonEmpty(functionARN, function.Name),
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		Name:         strings.TrimSpace(function.Name),
		State:        strings.TrimSpace(function.State),
		Tags:         function.Tags,
		Attributes: map[string]any{
			"architectures":      cloneStrings(function.Architectures),
			"code_sha256":        strings.TrimSpace(function.CodeSHA256),
			"code_size":          function.CodeSize,
			"description":        strings.TrimSpace(function.Description),
			"environment":        s.redactedEnvironment(function.Environment),
			"handler":            strings.TrimSpace(function.Handler),
			"image_uri":          strings.TrimSpace(function.ImageURI),
			"kms_key_arn":        strings.TrimSpace(function.KMSKeyARN),
			"last_modified":      timeOrNil(function.LastModified),
			"last_update_status": strings.TrimSpace(function.LastUpdateStatus),
			"logging_config":     loggingConfigMap(function.LoggingConfig),
			"memory_size":        function.MemorySize,
			"package_type":       strings.TrimSpace(function.PackageType),
			"resolved_image_uri": strings.TrimSpace(function.ResolvedImageURI),
			"role_arn":           strings.TrimSpace(function.RoleARN),
			"runtime":            strings.TrimSpace(function.Runtime),
			"source_kms_key_arn": strings.TrimSpace(function.SourceKMSKeyARN),
			"timeout_seconds":    function.TimeoutSeconds,
			"version":            strings.TrimSpace(function.Version),
			"vpc_config":         vpcConfigMap(function.VPCConfig),
		},
		CorrelationAnchors: []string{
			functionARN,
			strings.TrimSpace(function.Name),
			strings.TrimSpace(function.ImageURI),
			strings.TrimSpace(function.ResolvedImageURI),
		},
		SourceRecordID: firstNonEmpty(functionARN, function.Name),
	}
}

func (s Scanner) redactedEnvironment(environment map[string]string) map[string]any {
	if len(environment) == 0 {
		return nil
	}
	output := make(map[string]any, len(environment))
	for key, value := range environment {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		output[name] = redactionMap(redact.String(
			value,
			redact.ReasonKnownSensitiveKey,
			"lambda.environment."+name,
			s.RedactionKey,
		))
	}
	return output
}

func redactionMap(value redact.Value) map[string]string {
	return map[string]string{
		"marker": value.Marker,
		"reason": value.Reason,
		"source": value.Source,
	}
}

func aliasEnvelopes(
	boundary awscloud.Boundary,
	function Function,
	alias Alias,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(aliasObservation(boundary, function, alias))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := aliasFunctionRelationship(boundary, function, alias); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func aliasObservation(
	boundary awscloud.Boundary,
	function Function,
	alias Alias,
) awscloud.ResourceObservation {
	aliasARN := strings.TrimSpace(alias.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          aliasARN,
		ResourceID:   firstNonEmpty(aliasARN, strings.TrimSpace(function.ARN)+":"+strings.TrimSpace(alias.Name)),
		ResourceType: awscloud.ResourceTypeLambdaAlias,
		Name:         strings.TrimSpace(alias.Name),
		Attributes: map[string]any{
			"description":      strings.TrimSpace(alias.Description),
			"function_arn":     firstNonEmpty(alias.FunctionARN, function.ARN),
			"function_version": strings.TrimSpace(alias.FunctionVersion),
			"revision_id":      strings.TrimSpace(alias.RevisionID),
			"routing_weights":  cloneFloatMap(alias.RoutingWeights),
		},
		CorrelationAnchors: []string{aliasARN, strings.TrimSpace(alias.Name), firstNonEmpty(alias.FunctionARN, function.ARN)},
		SourceRecordID:     firstNonEmpty(aliasARN, strings.TrimSpace(function.ARN)+":"+strings.TrimSpace(alias.Name)),
	}
}

func eventSourceMappingEnvelopes(
	boundary awscloud.Boundary,
	function Function,
	mapping EventSourceMapping,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(eventSourceMappingObservation(boundary, function, mapping))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := eventSourceMappingFunctionRelationship(boundary, function, mapping); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func eventSourceMappingObservation(
	boundary awscloud.Boundary,
	function Function,
	mapping EventSourceMapping,
) awscloud.ResourceObservation {
	mappingID := firstNonEmpty(mapping.ARN, mapping.UUID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(mapping.ARN),
		ResourceID:   mappingID,
		ResourceType: awscloud.ResourceTypeLambdaEventSourceMapping,
		Name:         strings.TrimSpace(mapping.UUID),
		State:        strings.TrimSpace(mapping.State),
		Attributes: map[string]any{
			"batch_size":             mapping.BatchSize,
			"event_source_arn":       strings.TrimSpace(mapping.EventSourceARN),
			"function_arn":           firstNonEmpty(mapping.FunctionARN, function.ARN),
			"last_processing_result": strings.TrimSpace(mapping.LastProcessingResult),
			"maximum_retry_attempts": mapping.MaximumRetryAttempts,
			"parallelization_factor": mapping.ParallelizationFactor,
			"starting_position":      strings.TrimSpace(mapping.StartingPosition),
			"uuid":                   strings.TrimSpace(mapping.UUID),
		},
		CorrelationAnchors: []string{mappingID, strings.TrimSpace(mapping.EventSourceARN), firstNonEmpty(mapping.FunctionARN, function.ARN)},
		SourceRecordID:     mappingID,
	}
}
