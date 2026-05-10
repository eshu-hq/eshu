package terraformstate

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register(json.Number(""))
}

type collectFactSink struct {
	facts []facts.Envelope
}

func (s *collectFactSink) Emit(ctx context.Context, envelope facts.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.facts = append(s.facts, envelope)
	return nil
}

func (s *collectFactSink) Replay(ctx context.Context, sink FactSink) error {
	for _, envelope := range s.facts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sink.Emit(ctx, envelope); err != nil {
			return err
		}
	}
	return nil
}

func (s *collectFactSink) Facts() []facts.Envelope {
	return dedupeCollectedFacts(s.facts)
}

func dedupeCollectedFacts(input []facts.Envelope) []facts.Envelope {
	if len(input) == 0 {
		return input
	}
	last := make(map[string]int, len(input))
	for i, envelope := range input {
		last[envelope.FactID] = i
	}
	if len(last) == len(input) {
		return input
	}
	output := make([]facts.Envelope, 0, len(last))
	for i, envelope := range input {
		if last[envelope.FactID] == i {
			output = append(output, envelope)
		}
	}
	return output
}

type factSpool struct {
	file    *os.File
	encoder *gob.Encoder
	count   int
	path    string
}

func newFactSpool() (*factSpool, error) {
	file, err := os.CreateTemp("", "eshu-tfstate-facts-*.gob")
	if err != nil {
		return nil, fmt.Errorf("create terraform state fact spool: %w", err)
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
		return fmt.Errorf("write terraform state fact spool: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.count++
	return nil
}

func (s *factSpool) Replay(ctx context.Context, sink FactSink) error {
	if s.count == 0 {
		return nil
	}
	if _, err := s.file.Seek(0, 0); err != nil {
		return fmt.Errorf("rewind terraform state fact spool: %w", err)
	}
	decoder := gob.NewDecoder(s.file)
	for i := 0; i < s.count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var envelope facts.Envelope
		if err := decoder.Decode(&envelope); err != nil {
			return fmt.Errorf("read terraform state fact spool: %w", err)
		}
		if err := sink.Emit(ctx, envelope); err != nil {
			return err
		}
	}
	return nil
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
