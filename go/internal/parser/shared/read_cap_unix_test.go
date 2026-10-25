// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package shared

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestReadFileCappedRefusesAStreamThatGrowsPastTheCap (#8986 review P3-1): the
// size check before the read sees what Stat reports, which a FIFO (or a file
// that grows after the check) understates as 0. The read then stops one byte
// past the cap and refuses the body, so the post-read check is what bounds
// memory. A stream of exactly the cap still reads whole. Not parallel: it
// lowers the process-wide cap.
func TestReadFileCappedRefusesAStreamThatGrowsPastTheCap(t *testing.T) {
	defer SetSourceReadCapForTest(1024)()

	for _, cell := range []struct {
		name    string
		written int
		wantErr bool
	}{
		{name: "past the cap", written: 5000, wantErr: true},
		{name: "exactly the cap", written: 1024},
	} {
		t.Run(cell.name, func(t *testing.T) {
			fifo := filepath.Join(t.TempDir(), "stream.py")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Skipf("mkfifo unsupported here: %v", err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
				if err != nil {
					return
				}
				// The reader stops at cap+1, so a short or broken-pipe write is
				// expected for the oversize cell.
				_, _ = writer.Write(bytes.Repeat([]byte("z"), cell.written))
				_ = writer.Close()
			}()
			body, err := ReadFileCapped(fifo)
			<-done
			if cell.wantErr {
				if !errors.Is(err, ErrSourceTooLarge) || body != nil {
					t.Fatalf("ReadFileCapped(stream of %d) = (%d bytes, %v), want (nil, ErrSourceTooLarge)", cell.written, len(body), err)
				}
				return
			}
			if err != nil || len(body) != cell.written {
				t.Fatalf("ReadFileCapped(stream of %d) = (%d bytes, %v), want the whole body", cell.written, len(body), err)
			}
		})
	}
}
