package telemetry

import (
	"slices"
	"testing"
)

func TestNewBootstrapConfiguresStableOTELNames(t *testing.T) {
	t.Parallel()

	got, err := NewBootstrap("collector-git")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v, want nil", err)
	}

	if got.ServiceName != "collector-git" {
		t.Fatalf("ServiceName = %q, want %q", got.ServiceName, "collector-git")
	}

	if got.ServiceNamespace != "eshu" {
		t.Fatalf("ServiceNamespace = %q, want %q", got.ServiceNamespace, "eshu")
	}

	if got.MeterName != "eshu/go/data-plane" {
		t.Fatalf("MeterName = %q, want %q", got.MeterName, "eshu/go/data-plane")
	}

	if got.TracerName != "eshu/go/data-plane" {
		t.Fatalf("TracerName = %q, want %q", got.TracerName, "eshu/go/data-plane")
	}

	if got.LoggerName != "eshu/go/data-plane" {
		t.Fatalf("LoggerName = %q, want %q", got.LoggerName, "eshu/go/data-plane")
	}

	if got.InstrumentationScopeName() != "eshu/go/internal/telemetry" {
		t.Fatalf("InstrumentationScopeName() = %q, want %q", got.InstrumentationScopeName(), "eshu/go/internal/telemetry")
	}
}

func TestBootstrapResourceAttributesAreCloned(t *testing.T) {
	t.Parallel()

	got, err := NewBootstrap("collector-git")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v, want nil", err)
	}

	attrs := got.ResourceAttributes()
	want := map[string]string{
		"service.name":      "collector-git",
		"service.namespace": "eshu",
	}
	if !slices.EqualFunc(sortedMapPairs(attrs), sortedMapPairs(want), func(a, b pair) bool {
		return a.key == b.key && a.value == b.value
	}) {
		t.Fatalf("ResourceAttributes() = %v, want %v", attrs, want)
	}

	attrs["service.name"] = "mutated"
	if got.ResourceAttributes()["service.name"] != "collector-git" {
		t.Fatalf("ResourceAttributes() returned shared storage")
	}
}

func TestNewBootstrapRejectsBlankServiceName(t *testing.T) {
	t.Parallel()

	if _, err := NewBootstrap("   "); err == nil {
		t.Fatal("NewBootstrap() error = nil, want non-nil")
	}
}

type pair struct {
	key   string
	value string
}

func sortedMapPairs(values map[string]string) []pair {
	pairs := make([]pair, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, pair{key: key, value: value})
	}

	slices.SortFunc(pairs, func(a, b pair) int {
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		if a.value < b.value {
			return -1
		}
		if a.value > b.value {
			return 1
		}
		return 0
	})

	return pairs
}
