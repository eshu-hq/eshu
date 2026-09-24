// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake

import (
	"strings"
	"testing"
)

func TestCheckPlaceholders(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		query    string
		argCount int
		wantErr  string
	}{
		{name: "dense and matching", query: "UPDATE t SET a = $1, b = $2 WHERE c = $3", argCount: 3},
		{name: "repeated placeholder", query: "SELECT $1, $2 WHERE x = $1", argCount: 2},
		{name: "no placeholders no args", query: "SELECT 1", argCount: 0},
		{name: "more args than placeholders", query: "SELECT $1", argCount: 2, wantErr: "max placeholder = $1, args = 2"},
		{name: "fewer args than placeholders", query: "SELECT $1, $2, $3", argCount: 2, wantErr: "max placeholder = $3, args = 2"},
		{name: "skipped placeholder", query: "SELECT $1, $3", argCount: 3, wantErr: "skips placeholder $2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := CheckPlaceholders(tc.query, tc.argCount)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckPlaceholders() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("CheckPlaceholders() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
