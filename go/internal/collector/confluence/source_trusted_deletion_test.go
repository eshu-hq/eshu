// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestSourceMapsAllowEmptySuccessorToTrustedDeletion proves the escape hatch
// wiring: only a source that explicitly attests its deletions carries the
// trusted-deletion signal the store backstop honors.
func TestSourceMapsAllowEmptySuccessorToTrustedDeletion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	collect := func(t *testing.T, allowEmpty bool) scope.ScopeGeneration {
		t.Helper()
		source := Source{
			Client: &fakeClient{
				space: Space{
					ID:   "100",
					Key:  "PLAT",
					Name: "Platform",
					Links: Links{
						Base: "https://example.atlassian.net/wiki",
					},
				},
				spacePages: []Page{
					confluencePage("123", "Payment Service Deployment", 17, `<p>Deployment notes.</p>`),
				},
			},
			Config: SourceConfig{
				BaseURL:             "https://example.atlassian.net/wiki",
				SpaceID:             "100",
				SpaceKey:            "PLAT",
				AllowEmptySuccessor: allowEmpty,
				Now:                 func() time.Time { return observedAt },
			},
		}
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("Next() ok = false, want true")
		}
		return collected.Generation
	}
	if got := collect(t, false); got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without AllowEmptySuccessor, want false")
	}
	if got := collect(t, true); !got.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
}
