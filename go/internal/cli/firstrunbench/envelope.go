// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ReadEnvelope reads the envelope bytes from a file path or, when the path is
// empty, from the provided reader (stdin). Decoding belongs to
// firstrun.ParseEnvelope, which owns the canonical `{data, truth, error}`
// contract; this helper only fetches the bytes. The demo-benchmark family
// reads its own envelope through the same helper via the cmd/eshu wrapper.
func ReadEnvelope(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read envelope from stdin: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local benchmark artifact path, not an HTTP request param //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read envelope file %q: %w", path, err)
	}
	return raw, nil
}
