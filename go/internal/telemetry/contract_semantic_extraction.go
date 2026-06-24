package telemetry

import "slices"

const (
	// MetricDimensionSourceClass labels semantic extraction and source-facing
	// metrics with a bounded source class such as documentation or code_hints.
	MetricDimensionSourceClass = "source_class"
	// MetricDimensionProviderKind labels semantic extraction metrics with a
	// bounded provider kind such as deepseek, ollama, or openai_compatible.
	MetricDimensionProviderKind = "provider_kind"
	// MetricDimensionProviderProfileClass labels semantic extraction metrics
	// with a bounded profile class such as hosted or local.
	MetricDimensionProviderProfileClass = "provider_profile_class"
	// MetricDimensionBudgetState labels semantic extraction budget metrics with
	// a closed budget state such as allowed or exhausted.
	MetricDimensionBudgetState = "budget_state"
	// MetricDimensionBudgetReason labels semantic extraction budget metrics with
	// a closed budget reason such as allowed or daily_limit.
	MetricDimensionBudgetReason = "budget_reason"
)

const (
	// SpanSemanticExtractionQueueApply wraps deterministic semantic queue plan
	// application and stale-row protection.
	SpanSemanticExtractionQueueApply = "semantic_extraction.queue.apply"
	// SpanSemanticExtractionQueueClaim wraps provider-job claim selection and
	// lease fencing.
	SpanSemanticExtractionQueueClaim = "semantic_extraction.queue.claim"
	// SpanSemanticExtractionQueueComplete wraps retry, dead-letter, or success
	// lifecycle completion behind a lease fence.
	SpanSemanticExtractionQueueComplete = "semantic_extraction.queue.complete"
)

const (
	// LogKeySemanticExtractionStatus carries the bounded semantic queue status.
	LogKeySemanticExtractionStatus = "semantic_extraction.status"
	// LogKeySemanticExtractionSourceClass carries the bounded semantic source
	// class; raw source identifiers stay out of logs unless hashed separately.
	LogKeySemanticExtractionSourceClass = "semantic_extraction.source_class"
	// LogKeySemanticExtractionProviderKind carries the bounded provider kind.
	LogKeySemanticExtractionProviderKind = "semantic_extraction.provider_kind"
	// LogKeySemanticExtractionProviderProfileClass carries the bounded provider
	// profile class; provider profile IDs stay out of metric labels.
	LogKeySemanticExtractionProviderProfileClass = "semantic_extraction.provider_profile_class"
	// LogKeySemanticExtractionBudgetState carries the bounded semantic budget
	// state.
	LogKeySemanticExtractionBudgetState = "semantic_extraction.budget_state"
	// LogKeySemanticExtractionBudgetReason carries the bounded semantic budget
	// reason.
	LogKeySemanticExtractionBudgetReason = "semantic_extraction.budget_reason"
)

func init() {
	insertMetricDimensionsAfter(
		MetricDimensionSource,
		MetricDimensionSourceClass,
	)
	insertMetricDimensionsAfter(
		MetricDimensionProvider,
		MetricDimensionProviderKind,
		MetricDimensionProviderProfileClass,
	)
	insertMetricDimensionsAfter(
		MetricDimensionPrincipalKind,
		MetricDimensionBudgetState,
		MetricDimensionBudgetReason,
	)
	insertSpanNamesAfter(
		SpanReducerBatchClaim,
		SpanSemanticExtractionQueueApply,
		SpanSemanticExtractionQueueClaim,
		SpanSemanticExtractionQueueComplete,
	)
	logKeys = append(
		logKeys,
		LogKeySemanticExtractionStatus,
		LogKeySemanticExtractionSourceClass,
		LogKeySemanticExtractionProviderKind,
		LogKeySemanticExtractionProviderProfileClass,
		LogKeySemanticExtractionBudgetState,
		LogKeySemanticExtractionBudgetReason,
	)
}

func insertMetricDimensionsAfter(anchor string, values ...string) {
	for idx, key := range metricDimensionKeys {
		if key == anchor {
			metricDimensionKeys = slices.Insert(metricDimensionKeys, idx+1, values...)
			return
		}
	}
	metricDimensionKeys = append(metricDimensionKeys, values...)
}

func insertSpanNamesAfter(anchor string, values ...string) {
	for idx, name := range spanNames {
		if name == anchor {
			spanNames = slices.Insert(spanNames, idx+1, values...)
			return
		}
	}
	spanNames = append(spanNames, values...)
}
