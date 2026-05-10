package tfstateruntime

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const terraformStateFactStreamBuffer = 500

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register(json.Number(""))
}

type factSpool struct {
	file    *os.File
	encoder *gob.Encoder
	count   int
	path    string
	mu      sync.Mutex
	err     error
}

func newFactSpool() (*factSpool, error) {
	file, err := os.CreateTemp("", "eshu-tfstate-runtime-facts-*.gob")
	if err != nil {
		return nil, fmt.Errorf("create terraform state runtime fact spool: %w", err)
	}
	return &factSpool{
		file:    file,
		encoder: gob.NewEncoder(file),
		path:    file.Name(),
	}, nil
}

func (s *factSpool) Emit(ctx context.Context, envelope facts.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.encoder.Encode(envelope); err != nil {
		return fmt.Errorf("write terraform state runtime fact spool: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.count++
	return nil
}

func (s *factSpool) Stream(ctx context.Context) (<-chan facts.Envelope, func() error) {
	out := make(chan facts.Envelope, terraformStateFactStreamBuffer)
	done := make(chan struct{})
	go func() {
		defer close(out)
		defer close(done)
		defer s.Close()
		if _, err := s.file.Seek(0, 0); err != nil {
			s.setErr(fmt.Errorf("rewind terraform state runtime fact spool: %w", err))
			return
		}
		decoder := gob.NewDecoder(s.file)
		for i := 0; i < s.count; i++ {
			var envelope facts.Envelope
			if err := decoder.Decode(&envelope); err != nil {
				s.setErr(fmt.Errorf("read terraform state runtime fact spool: %w", err))
				return
			}
			select {
			case <-ctx.Done():
				s.setErr(ctx.Err())
				return
			case out <- envelope:
			}
		}
	}()
	return out, func() error {
		<-done
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.err
	}
}

func (s *factSpool) setErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *factSpool) Close() {
	if s == nil {
		return
	}
	if s.file != nil {
		_ = s.file.Close()
	}
	if s.path != "" {
		_ = os.Remove(s.path)
	}
}

var _ terraformstate.FactSink = (*factSpool)(nil)
