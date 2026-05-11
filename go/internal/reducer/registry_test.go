package reducer

import "testing"

// TestOwnershipShapeValidateAcceptsCounterEmitWithoutCanonicalWrite pins the
// contract chunk #43 introduced: a reducer domain whose v1 truth surface is
// bounded counter+log emission can register with CanonicalWrite=false as long
// as CounterEmit=true. Cross-source and cross-scope remain required.
func TestOwnershipShapeValidateAcceptsCounterEmitWithoutCanonicalWrite(t *testing.T) {
	t.Parallel()

	shape := OwnershipShape{
		CrossSource:    true,
		CrossScope:     true,
		CanonicalWrite: false,
		CounterEmit:    true,
	}
	if err := shape.Validate(); err != nil {
		t.Fatalf("Validate(CrossSource+CrossScope+CounterEmit) = %v, want nil", err)
	}
}

// TestOwnershipShapeValidateAcceptsCanonicalWriteWithoutCounterEmit covers the
// pre-existing canonical-write domains. Without CounterEmit they still
// validate because CanonicalWrite alone satisfies the truth-surface rule.
func TestOwnershipShapeValidateAcceptsCanonicalWriteWithoutCounterEmit(t *testing.T) {
	t.Parallel()

	shape := OwnershipShape{
		CrossSource:    true,
		CrossScope:     true,
		CanonicalWrite: true,
		CounterEmit:    false,
	}
	if err := shape.Validate(); err != nil {
		t.Fatalf("Validate(CrossSource+CrossScope+CanonicalWrite) = %v, want nil", err)
	}
}

// TestOwnershipShapeValidateRejectsNoTruthSurface pins the other side of the
// contract: cross-source+cross-scope alone is not enough — a domain must
// declare at least one of CanonicalWrite or CounterEmit. Without either,
// register() callers would have no observable truth surface, which is the
// failure mode the chunk #43 review flagged.
func TestOwnershipShapeValidateRejectsNoTruthSurface(t *testing.T) {
	t.Parallel()

	shape := OwnershipShape{
		CrossSource:    true,
		CrossScope:     true,
		CanonicalWrite: false,
		CounterEmit:    false,
	}
	err := shape.Validate()
	if err == nil {
		t.Fatal("Validate(CrossSource+CrossScope only) = nil, want non-nil")
	}
}

// TestOwnershipShapeValidateRejectsMissingCrossSource and
// TestOwnershipShapeValidateRejectsMissingCrossScope guard the other two
// reducer-boundary invariants. They predate chunk #43 but are not exercised
// by any existing test; CounterEmit additions are a good moment to pin them.
func TestOwnershipShapeValidateRejectsMissingCrossSource(t *testing.T) {
	t.Parallel()

	shape := OwnershipShape{
		CrossSource:    false,
		CrossScope:     true,
		CanonicalWrite: true,
	}
	if err := shape.Validate(); err == nil {
		t.Fatal("Validate(!CrossSource) = nil, want non-nil")
	}
}

func TestOwnershipShapeValidateRejectsMissingCrossScope(t *testing.T) {
	t.Parallel()

	shape := OwnershipShape{
		CrossSource:    true,
		CrossScope:     false,
		CanonicalWrite: true,
	}
	if err := shape.Validate(); err == nil {
		t.Fatal("Validate(!CrossScope) = nil, want non-nil")
	}
}
