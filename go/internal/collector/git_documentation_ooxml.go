package collector

import "github.com/eshu-hq/eshu/go/internal/collector/ooxmlpreflight"

func ooxmlPreflightBlocksExtraction(result ooxmlpreflight.Result) bool {
	for _, warning := range result.Warnings {
		switch warning.Class {
		case "", ooxmlpreflight.WarningAnnotationTextSkipped, ooxmlpreflight.WarningHiddenContentSkipped:
			continue
		default:
			return true
		}
	}
	return false
}
