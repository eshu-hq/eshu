// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import "testing"

func TestEvaluateQueryShapeNestedArrayBounds(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"data", "truth", "error"},
		RequiredJSONArrayBounds: map[string]QueryArrayBound{
			"data.categories": {
				MinimumResults:           7,
				MaximumResults:           7,
				ResultItemRequiredFields: []string{"category", "counts", "unavailable"},
			},
		},
	}
	for _, test := range []struct {
		name string
		body string
		ok   bool
	}{
		{"seven complete", `{"data":{"categories":[{"category":"a","counts":{},"unavailable":false},{"category":"b","counts":{},"unavailable":false},{"category":"c","counts":{},"unavailable":false},{"category":"d","counts":{},"unavailable":false},{"category":"e","counts":{},"unavailable":false},{"category":"f","counts":{},"unavailable":false},{"category":"g","counts":{},"unavailable":false}]},"truth":{},"error":null}`, true},
		{"six categories", `{"data":{"categories":[{},{},{},{},{},{}]},"truth":{},"error":null}`, false},
		{"eight categories", `{"data":{"categories":[{},{},{},{},{},{},{},{}]},"truth":{},"error":null}`, false},
		{"missing item field", `{"data":{"categories":[{"category":"a","counts":{},"unavailable":false},{"category":"b","counts":{},"unavailable":false},{"category":"c","counts":{},"unavailable":false},{"category":"d","counts":{},"unavailable":false},{"category":"e","counts":{},"unavailable":false},{"category":"f","counts":{},"unavailable":false},{"category":"g","counts":{}}]},"truth":{},"error":null}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateQueryShape(test.name, shape, []byte(test.body))
			if got.OK != test.ok || !got.Required {
				t.Fatalf("EvaluateQueryShape = %+v, want OK=%t and required", got, test.ok)
			}
		})
	}
}
