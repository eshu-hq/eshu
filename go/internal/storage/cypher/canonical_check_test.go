package cypher

import (
	"context"
	"fmt"
	"testing"
)

type fakeCypherReader struct {
	exists bool
	err    error
}

func (f *fakeCypherReader) QueryCypherExists(_ context.Context, _ string, _ map[string]any) (bool, error) {
	return f.exists, f.err
}

func TestCanonicalNodeCheckerReturnsTrueWhenNodesExist(t *testing.T) {
	t.Parallel()

	checker := NewCanonicalNodeChecker(&fakeCypherReader{exists: true})
	got, err := checker.HasCanonicalCodeTargets(context.Background())
	if err != nil {
		t.Fatalf("HasCanonicalCodeTargets() error = %v", err)
	}
	if !got {
		t.Fatal("HasCanonicalCodeTargets() = false, want true")
	}
}

func TestCanonicalNodeCheckerReturnsFalseWhenNoNodes(t *testing.T) {
	t.Parallel()

	checker := NewCanonicalNodeChecker(&fakeCypherReader{exists: false})
	got, err := checker.HasCanonicalCodeTargets(context.Background())
	if err != nil {
		t.Fatalf("HasCanonicalCodeTargets() error = %v", err)
	}
	if got {
		t.Fatal("HasCanonicalCodeTargets() = true, want false")
	}
}

func TestCanonicalNodeCheckerPropagatesError(t *testing.T) {
	t.Parallel()

	checker := NewCanonicalNodeChecker(&fakeCypherReader{
		exists: false,
		err:    fmt.Errorf("connection refused"),
	})
	_, err := checker.HasCanonicalCodeTargets(context.Background())
	if err == nil {
		t.Fatal("HasCanonicalCodeTargets() error = nil, want error")
	}
}

func TestCanonicalNodeCheckerNilReaderReturnsError(t *testing.T) {
	t.Parallel()

	checker := NewCanonicalNodeChecker(nil)
	_, err := checker.HasCanonicalCodeTargets(context.Background())
	if err == nil {
		t.Fatal("HasCanonicalCodeTargets(nil reader) error = nil, want error")
	}
}
