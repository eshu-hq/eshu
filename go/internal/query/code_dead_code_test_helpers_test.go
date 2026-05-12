package query

import "testing"

func assertQueryTestStringSliceEqual(t *testing.T, got any, want []string) {
	t.Helper()

	gotSlice, ok := got.([]any)
	if !ok {
		t.Fatalf("string slice type = %T, want []any", got)
	}
	if len(gotSlice) != len(want) {
		t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
	}
	for i, wantValue := range want {
		if gotValue, ok := gotSlice[i].(string); !ok || gotValue != wantValue {
			t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
		}
	}
}
