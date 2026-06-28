// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"bytes"
	"fmt"
	"io"
)

// readAndClose reads all of r and closes it, returning the bytes. It always
// closes r, even when the read fails, so a recorded request/response body never
// leaks the underlying reader.
func readAndClose(r io.ReadCloser) ([]byte, error) {
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// newBodyReader returns a fresh ReadCloser over data so a body can be replayed
// or re-read after capture. It is a no-op Close over an in-memory buffer.
func newBodyReader(data []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(data))
}
