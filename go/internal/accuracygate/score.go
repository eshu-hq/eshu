package accuracygate

import "sort"

// LabeledPrediction is one observed-vs-expected outcome used to score a
// precision/recall dimension from a confusion matrix. Label identifies the item
// (a case id, language, or evidence kind) for the published per-item detail.
// Expected and Observed are the golden and measured classifications; Positive
// reports whether Expected is the class the dimension treats as a positive
// (e.g. an "admitted" correlation, or a correctly-scored language).
type LabeledPrediction struct {
	Label    string
	Expected string
	Observed string
	Positive bool
}

// ScorePredictions turns a set of labeled predictions into a Metric. An item is
// a true positive when it is Positive and Observed == Expected; a false negative
// when it is Positive and Observed != Expected; a false positive when it is not
// Positive yet Observed equals the positive expectation of some positive item,
// i.e. the prediction crossed into the positive class wrongly.
//
// Because correlation and complexity classes are multi-valued (admitted /
// rejected / ambiguous / ...; scored / unscored), ScorePredictions derives the
// positive class label from the Positive flag rather than a hardcoded string:
//   - precision = correctPositives / observedPositives,
//   - recall    = correctPositives / expectedPositives.
//
// CoveredItems is the count of expected-positive items, the dimension's gated
// coverage. Labels record each item's "expected->observed" so the published
// report shows exactly which item moved. The div-by-zero convention matches
// goldenaudit: an empty denominator scores 1.0 only when its counterpart is also
// empty, else 0.0.
func ScorePredictions(predictions []LabeledPrediction) Metric {
	positiveLabels := positiveExpectations(predictions)

	var correctPositives, observedPositives, expectedPositives int
	labels := make(map[string]string, len(predictions))
	for _, prediction := range predictions {
		labels[prediction.Label] = prediction.Expected + "->" + prediction.Observed
		if prediction.Positive {
			expectedPositives++
			if prediction.Observed == prediction.Expected {
				correctPositives++
			}
		}
		// An observed value that matches any positive expectation counts as an
		// observed positive, whether or not this item was expected positive.
		if _, ok := positiveLabels[prediction.Observed]; ok {
			observedPositives++
		}
	}

	return Metric{
		Precision:    ratio(correctPositives, observedPositives, expectedPositives == 0),
		Recall:       ratio(correctPositives, expectedPositives, observedPositives == 0),
		CoveredItems: expectedPositives,
		Labels:       labels,
	}
}

// positiveExpectations collects the set of Expected values that belong to the
// positive class, used to decide whether an observed value is a positive.
func positiveExpectations(predictions []LabeledPrediction) map[string]struct{} {
	set := make(map[string]struct{})
	for _, prediction := range predictions {
		if prediction.Positive {
			set[prediction.Expected] = struct{}{}
		}
	}
	return set
}

// ratio divides numerator by denominator with the goldenaudit div-by-zero
// convention: an empty denominator scores 1.0 when emptyCounterpart is true and
// 0.0 otherwise.
func ratio(numerator int, denominator int, emptyCounterpart bool) float64 {
	if denominator == 0 {
		if emptyCounterpart {
			return 1.0
		}
		return 0.0
	}
	return float64(numerator) / float64(denominator)
}

// CoverageMetric builds a Metric for a coverage-style dimension where each item
// is either correct (scored as intended) or not. correctLabels are the items
// that met their golden expectation; allLabels are every item in scope, mapped
// to a short status string for the published detail. Precision and recall are
// both correct/total so a single dropped item visibly lowers the dimension, and
// CoveredItems is the correct count.
func CoverageMetric(correctLabels []string, allLabels map[string]string) Metric {
	correct := make(map[string]struct{}, len(correctLabels))
	for _, label := range correctLabels {
		correct[label] = struct{}{}
	}
	total := len(allLabels)
	correctInScope := 0
	for label := range allLabels {
		if _, ok := correct[label]; ok {
			correctInScope++
		}
	}
	score := ratio(correctInScope, total, total == 0)
	return Metric{
		Precision:    score,
		Recall:       score,
		CoveredItems: correctInScope,
		Labels:       allLabels,
	}
}

// SortedLabels returns a metric's label keys sorted, for deterministic rendering
// outside JSON marshaling.
func SortedLabels(metric Metric) []string {
	keys := make([]string, 0, len(metric.Labels))
	for key := range metric.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
