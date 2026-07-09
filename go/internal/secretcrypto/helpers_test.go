// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretcrypto_test

import (
	"os"
	"testing"
)

// writeTestFile writes body to path with operator-file-like permissions,
// mirroring how a mounted secret file would be delivered in deployment.
func writeTestFile(t *testing.T, path string, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o600)
}
