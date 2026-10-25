// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// MaxSourceBytes is the per-file read cap for parser input (#8986). A file
// larger than this never reaches a parser: ReadFileCapped and ReadSource
// return ErrSourceTooLarge instead of its bytes, and the git collector skips
// the file at discovery from its recorded size.
//
// The value is 64 MiB. The largest lock file in the repository and the golden
// corpus measured 274,035 bytes (the root package-lock.json), and the largest
// golden-corpus lock file 743 bytes, so the cap leaves more than two hundred
// times headroom over every measured lock file while bounding the memory one
// parse worker can spend on a single hostile or generated file. Per-parser caps
// (for example the 2 MiB Gradle catalog cap and the 1 MiB JavaScript and PHP
// tree-sitter caps) still apply below it.
const MaxSourceBytes int64 = 64 << 20

// ErrSourceTooLarge reports a file over the per-file read cap. Callers match it
// with errors.Is to tell a deterministic size skip from a read failure.
var ErrSourceTooLarge = errors.New("source file exceeds the per-file read cap")

// sourceReadCapOverride holds a test override of the cap; zero means
// MaxSourceBytes. Production code never sets it.
var sourceReadCapOverride atomic.Int64

// SourceReadCap returns the per-file read cap in force: MaxSourceBytes unless
// a test lowered it with SetSourceReadCapForTest.
func SourceReadCap() int64 {
	if override := sourceReadCapOverride.Load(); override > 0 {
		return override
	}
	return MaxSourceBytes
}

// SetSourceReadCapForTest lowers the process-wide read cap to capBytes so a
// test can exercise the cap with small files, and returns a function that
// restores the previous value. Test-only: production code never calls it. A
// test that calls it must not run in parallel with other tests, because every
// parse in the process sees the lowered cap.
func SetSourceReadCapForTest(capBytes int64) func() {
	previous := sourceReadCapOverride.Swap(capBytes)
	return func() { sourceReadCapOverride.Store(previous) }
}

// ReadFileCapped reads path whole when it is at most SourceReadCap bytes and
// returns an error wrapping ErrSourceTooLarge, with no body, when it is
// larger. It checks the size the open file reports before reading, then reads
// at most one byte past the cap, so a file that grew after the size check is
// still refused without being read whole. The buffer is sized from the
// reported size, so a file under the cap costs the same allocation as
// os.ReadFile.
func ReadFileCapped(path string) ([]byte, error) {
	capBytes := SourceReadCap()
	file, err := os.Open(path) // #nosec G304 -- reads an indexed repository source file at a path derived from the scan target
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var size int64
	if info, statErr := file.Stat(); statErr == nil {
		size = info.Size()
	}
	if size > capBytes {
		return nil, fmt.Errorf("%w: %d bytes, cap %d bytes", ErrSourceTooLarge, size, capBytes)
	}
	body, err := readAllInto(io.LimitReader(file, capBytes+1), make([]byte, 0, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > capBytes {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrSourceTooLarge, capBytes)
	}
	return body, nil
}

// readAllInto reads r to EOF, appending to buf and growing it only when full.
func readAllInto(r io.Reader, buf []byte) ([]byte, error) {
	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, nil
			}
			return nil, err
		}
	}
}
