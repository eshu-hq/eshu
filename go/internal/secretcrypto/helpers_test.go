// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretcrypto_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// writeTestFile writes body to path with operator-file-like permissions,
// mirroring how a mounted secret file would be delivered in deployment.
func writeTestFile(t *testing.T, path string, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o600)
}

// envelopeKeyID extracts the key_id field from an ESK1 envelope for
// assertions that only care about which key sealed it.
func envelopeKeyID(envelope string) (string, error) {
	parts := strings.Split(envelope, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("envelope %q does not have 4 dot-separated parts", envelope)
	}
	return parts[1], nil
}
