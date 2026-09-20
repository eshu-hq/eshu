// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// tableSource is a budget table read once, so the bytes the gate parses are
// exactly the bytes it fingerprints in its log.
type tableSource struct {
	path string
	data []byte
}

// readTable reads the budget table at path in one call.
func readTable(path string) (tableSource, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the -budgets/-work-budgets CLI flag, operator-controlled
	if err != nil {
		return tableSource{}, err
	}
	return tableSource{path: path, data: data}, nil
}

// reader returns a reader over the bytes readTable captured.
func (t tableSource) reader() io.Reader { return bytes.NewReader(t.data) }

// provenance returns "<path> sha256:<hex>" so a run's log records exactly
// which table it enforced.
func (t tableSource) provenance() string {
	sum := sha256.Sum256(t.data)
	return t.path + " sha256:" + hex.EncodeToString(sum[:])
}
