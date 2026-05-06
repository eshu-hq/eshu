package parser

import (
	"reflect"
	"testing"
)

func TestExtractRuntimeServiceDependencies(t *testing.T) {
	t.Parallel()

	source := `
services: ["checkout", "payments", "/api/redis-cache", "aws", "payments", "redis-cache", "${stage}-svc", "elastic"]
services: ['search', 'checkout']
`

	got := extractRuntimeServiceDependencies(source, "checkout")
	want := []string{"payments", "redis-cache", "search"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractRuntimeServiceDependencies() = %#v, want %#v", got, want)
	}
}
