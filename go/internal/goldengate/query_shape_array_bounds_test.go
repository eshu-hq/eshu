// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"fmt"
	"strings"
	"testing"
)

const completeCategory = `{"category":"active","counts":{},"unavailable":false}`

func categoryResponse(count int) string {
	items := make([]string, count)
	for index := range items {
		items[index] = completeCategory
	}
	return fmt.Sprintf(`{"data":{"categories":[%s]},"truth":{},"error":null}`, strings.Join(items, ","))
}

func TestEvaluateQueryShapeNestedArrayBounds(t *testing.T) {
	t.Parallel()

	bound := QueryArrayBound{
		MinimumResults:           7,
		MaximumResults:           7,
		ResultItemRequiredFields: []string{"category", "counts", "unavailable"},
	}
	shape := QueryShape{
		RequiredResponseFields: []string{"data", "truth", "error"},
		RequiredJSONArrayBounds: map[string]QueryArrayBound{
			"data.categories": bound,
		},
	}
	for _, test := range []struct {
		name        string
		body        string
		path        string
		wantFailure string
	}{
		{"seven complete", categoryResponse(7), "data.categories", ""},
		{"six categories", categoryResponse(6), "data.categories", "has 6 results"},
		{"eight categories", categoryResponse(8), "data.categories", "has 8 results"},
		{"missing item field", strings.Replace(categoryResponse(7), completeCategory, `{"category":"active","counts":{}}`, 1), "data.categories", "missing required field"},
		{"scalar with item fields required", `{"data":{"categories":["a","b","c","d","e","f","g"]},"truth":{},"error":null}`, "data.categories", "item 0 is not an object"},
		{"absent path", `{"data":{},"truth":{},"error":null}`, "data.categories", "resolved no values"},
		{"non-array path", `{"data":{"categories":{}},"truth":{},"error":null}`, "data.categories", "did not resolve an array"},
		{"wildcard resolves items", categoryResponse(7), "data.categories[]", "resolved 7 values"},
		{"wildcard resolves sibling arrays", fmt.Sprintf(`{"data":{"groups":[{"categories":[%s]},{"categories":[%s]}]},"truth":{},"error":null}`, completeCategory, completeCategory), "data.groups[].categories", "resolved 2 values"},
	} {
		t.Run(test.name, func(t *testing.T) {
			shape.RequiredJSONArrayBounds = map[string]QueryArrayBound{test.path: bound}
			got := EvaluateQueryShape(test.name, shape, []byte(test.body))
			if got.OK != (test.wantFailure == "") || !got.Required || !strings.Contains(got.Detail, test.wantFailure) {
				t.Fatalf("EvaluateQueryShape = %+v, want failure %q and required", got, test.wantFailure)
			}
		})
	}
}

func TestEvaluateQueryShapeNestedCountOnlyScalars(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"data", "truth", "error"},
		RequiredJSONArrayBounds: map[string]QueryArrayBound{
			"data.categories": {MinimumResults: 2, MaximumResults: 2},
		},
	}
	for _, test := range []struct {
		name        string
		body        string
		wantFailure string
	}{
		{"strings and numbers", `{"data":{"categories":["active",42]},"truth":{},"error":null}`, ""},
		{"too few scalars", `{"data":{"categories":["active"]},"truth":{},"error":null}`, "has 1 results"},
		{"too many scalars", `{"data":{"categories":["active",42,true]},"truth":{},"error":null}`, "has 3 results"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateQueryShape(test.name, shape, []byte(test.body))
			if got.OK != (test.wantFailure == "") || !got.Required || !strings.Contains(got.Detail, test.wantFailure) {
				t.Fatalf("EvaluateQueryShape = %+v, want failure %q and required", got, test.wantFailure)
			}
		})
	}
}
