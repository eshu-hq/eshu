// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import "testing"

// TestLoopbackHostsChecksEveryFallback: pgx dials fallback hosts in order,
// so a loopback first host does not make the DSN local.
func TestLoopbackHostsChecksEveryFallback(t *testing.T) {
	cases := map[string]bool{
		"postgres://u:p@127.0.0.1:5432/x":                                   true,
		"postgres://u:p@localhost:5432/x":                                   true,
		"postgres://u@127.0.0.1,core-db.example.internal:5432/x":            false,
		"postgres://u@127.0.0.1:15432,127.0.0.1:15433/x":                    true,
		"host=127.0.0.1,core-db.example.internal port=5432 dbname=x user=u": false,
		"postgres://u@10.0.0.5:5432/x":                                      false,
	}
	for dsn, ok := range cases {
		err := loopbackOnly(dsn)
		if ok && err != nil {
			t.Fatalf("%s: refused a loopback DSN: %v", dsn, err)
		}
		if !ok && err == nil {
			t.Fatalf("%s: accepted a DSN that can reach a non-loopback host", dsn)
		}
	}
}
