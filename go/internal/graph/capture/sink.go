// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

// capturePhaseEnvVar labels each recording with the replay stage that
// produced it (bootstrap, drain, or query), set per stage by the B-7
// orchestrator. It is a label only: the diff pairs recordings by backend,
// never by phase.
const capturePhaseEnvVar = "ESHU_DIFFERENTIAL_CAPTURE_PHASE"

// knownBackends is the closed pair the diff understands. A recording
// labeled anything else is a mislabeled run, and loading it onto either
// side of the diff would compare the wrong backends, so [LoadDir] rejects
// it instead of guessing.
var knownBackends = map[string]bool{"nornicdb": true, "neo4j": true}

// storedRecord is one JSONL line: the capture phase plus the statement
// record. The backend travels inside the record (set by the capture
// decorator), so a file moved between runs cannot be misattributed.
type storedRecord struct {
	Phase  string                                `json:"phase"`
	Record backendconformance.DifferentialRecord `json:"record"`
}

// Sink appends one process's differential records to a JSONL file. It is
// safe for concurrent use. The file opens lazily on the first [Sink.Append]
// as <binary>-<backend>-<pid>.jsonl inside the directory, so a process
// that executes no graph statements leaves no file behind.
type Sink struct {
	mu      sync.Mutex
	dir     string
	backend string
	binary  string
	phase   string
	file    *os.File
	writer  *bufio.Writer
	// firstErr keeps the first streaming failure: per-record Append
	// callers (the recorder stream) cannot act on the error mid-run, so
	// Close surfaces it instead of reporting a clean shutdown over a
	// truncated recording.
	firstErr error
}

// OpenDir creates dir and returns a sink recording backend executions from
// binary. The backend must be one of the known pair; anything else fails
// here, at the recording side, rather than at diff time.
func OpenDir(dir, backend, binary string) (*Sink, error) {
	if !knownBackends[backend] {
		return nil, fmt.Errorf("unknown differential backend %q", backend)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create differential capture dir: %w", err)
	}
	return &Sink{dir: dir, backend: backend, binary: binary, phase: os.Getenv(capturePhaseEnvVar)}, nil
}

// Append records one statement execution. A record labeled for a different
// backend than the sink is a wiring bug, and failing here keeps it from
// silently joining the wrong side of the diff.
func (s *Sink) Append(record backendconformance.DifferentialRecord) error {
	if record.Backend != s.backend {
		return fmt.Errorf("differential record backend %q does not match sink backend %q", record.Backend, s.backend)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstErr != nil {
		return s.firstErr
	}
	if s.writer == nil {
		path := filepath.Join(s.dir, fmt.Sprintf("%s-%s-%d.jsonl", s.binary, s.backend, os.Getpid()))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open differential capture file: %w", err)
		}
		s.file = file
		s.writer = bufio.NewWriter(file)
	}
	raw, err := json.Marshal(storedRecord{Phase: s.phase, Record: record})
	if err != nil {
		return s.fail(fmt.Errorf("encode differential record: %w", err))
	}
	if _, err := s.writer.Write(append(raw, '\n')); err != nil {
		return s.fail(fmt.Errorf("write differential record: %w", err))
	}
	// Flush every record: the replay binaries die by SIGTERM, so a record
	// must reach the file without waiting for Close. One flushed write per
	// statement is negligible next to the Bolt round trip that produced it.
	if err := s.writer.Flush(); err != nil {
		return s.fail(fmt.Errorf("flush differential record: %w", err))
	}
	return nil
}

// fail stashes the first streaming error for Close to surface. Callers
// hold s.mu.
func (s *Sink) fail(err error) error {
	if s.firstErr == nil {
		s.firstErr = err
	}
	return s.firstErr
}

// Close flushes and closes the sink, surfacing the first streaming error
// if any append failed mid-run. A sink with no appends wrote no file and
// closes cleanly.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstErr != nil {
		return s.firstErr
	}
	if s.writer == nil {
		return nil
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("flush differential capture file: %w", err)
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close differential capture file: %w", err)
	}
	s.writer = nil
	s.file = nil
	return nil
}

// LoadDir reads every recording file in dir and groups the records by
// backend. A record labeled outside the known pair fails the load: it is
// a mislabeled run, and guessing its side would compare the wrong
// backends.
func LoadDir(dir string) (map[string][]backendconformance.DifferentialRecord, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("list differential capture files: %w", err)
	}
	byBackend := make(map[string][]backendconformance.DifferentialRecord)
	for _, path := range matches {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open differential capture file %s: %w", path, err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var stored storedRecord
			if err := json.Unmarshal(scanner.Bytes(), &stored); err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("decode differential capture file %s line %d: %w", path, line, err)
			}
			if !knownBackends[stored.Record.Backend] {
				_ = file.Close()
				return nil, fmt.Errorf("differential capture file %s line %d: unknown backend %q", path, line, stored.Record.Backend)
			}
			byBackend[stored.Record.Backend] = append(byBackend[stored.Record.Backend], stored.Record)
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("read differential capture file %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close differential capture file %s: %w", path, err)
		}
	}
	return byBackend, nil
}
