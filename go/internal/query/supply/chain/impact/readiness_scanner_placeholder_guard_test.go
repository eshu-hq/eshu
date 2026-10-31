// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// TestScannerReadinessPlaceholderContract pins the positional-argument
// contract behind the scanner authorization predicate: the base readiness
// arguments own $1..$25 and the caller authorization arguments own $26..$31.
// A renumber on either side without the other fails here instead of leaking
// one tenant's scan facts into another tenant's readiness snapshot.
func TestScannerReadinessPlaceholderContract(t *testing.T) {
	base := readinessQueryArguments(ReadinessQuery{}, readinessTarget{}, time.Now().UTC())
	if len(base) != 25 {
		t.Fatalf("readiness base args=%d, want 25 ($1..$25)", len(base))
	}
	scoped := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "guard-tenant", WorkspaceID: "guard-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "guard-policy",
		AllowedScopeIDs: []string{"guard-scope"},
	})
	for name, ctx := range map[string]context.Context{
		"shared": context.Background(),
		"scoped": scoped,
	} {
		authArgs := supplyChainImpactCallerAuthorizationArgs(ctx)
		if len(authArgs) != 6 {
			t.Fatalf("%s caller auth args=%d, want 6 ($26..$31)", name, len(authArgs))
		}
	}
	for _, want := range []string{"$26", "$27", "$28", "$29", "$30", "$31"} {
		if !strings.Contains(listCurrentReadinessQuery, want) {
			t.Errorf("rendered readiness statement is missing %s with 31 args", want)
		}
	}
	if strings.Contains(listCurrentReadinessQuery, "$32") {
		t.Errorf("rendered readiness statement references $32 with only 31 args")
	}
}
