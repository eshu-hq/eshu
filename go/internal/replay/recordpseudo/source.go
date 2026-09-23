// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Report is what a recorder logs about one pseudonymized run. It carries
// counts, field paths and the key fingerprint -- never a raw or pseudonymized
// value.
type Report struct {
	// KeyFingerprint identifies the recording key (Key.Fingerprint).
	KeyFingerprint string
	// Scopes and Facts count what the wrapped source emitted.
	Scopes int
	Facts  int
	// Tokens is the dictionary size: distinct raw identifiers learned.
	Tokens int
	// Learned counts learned tokens per class.
	Learned map[Class]int
	// OpaquePaths are the field paths whose values were made opaque, sorted,
	// including every unclassified path.
	OpaquePaths []string
	// UnclassifiedPaths are the opaque paths whose key the policy does not
	// list at all: each one is a table gap for the collector owner to classify.
	UnclassifiedPaths []string
	// IPv4Collisions counts linear-probe steps taken in the RFC 5737 slot
	// space; a non-zero value means some address pseudonyms are order-dependent.
	IPv4Collisions int
	// Produced is the set of pseudonym tokens this run emitted, for Verify.
	Produced Set
}

// LogAttrs renders the report as slog key/value pairs for the
// collector.record.pseudonymized event.
func (r Report) LogAttrs() []any {
	learned := make(map[string]int, len(r.Learned))
	for class, n := range r.Learned {
		learned[class.String()] = n
	}
	return []any{
		"key_fingerprint", r.KeyFingerprint,
		"scopes", r.Scopes,
		"facts", r.Facts,
		"tokens", r.Tokens,
		"learned_by_class", learned,
		"opaque_paths", r.OpaquePaths,
		"unclassified_paths", r.UnclassifiedPaths,
		"ipv4_collisions", r.IPv4Collisions,
	}
}

// Set is the membership belt input: every pseudonym token a run produced.
type Set map[string]struct{}

// Has reports membership.
func (s Set) Has(token string) bool {
	_, ok := s[token]
	return ok
}

// Source is a collector.Source that pseudonymizes every generation of the
// wrapped source before a recorder sees it. The first Next drains the inner
// source completely, learns the dictionary from every generation, and only
// then rewrites -- so a token learned late is still rewritten in an earlier
// scope's structural fields.
type Source struct {
	inner   collector.Source
	dict    *dictionary
	walker  *walker
	report  *Report
	drained bool
	queue   []collector.CollectedGeneration
}

// Wrap returns the pseudonymizing source and the report it fills once the
// inner source is drained. The report's fields are valid after Next has
// reported the batch exhausted.
func Wrap(inner collector.Source, key Key, policy Policy) (*Source, *Report, error) {
	if inner == nil {
		return nil, nil, fmt.Errorf("recordpseudo: inner source is required")
	}
	cfg := Config{Key: key, Policy: policy}
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	dict := newDictionary(key)
	report := &Report{KeyFingerprint: key.Fingerprint(), Learned: map[Class]int{}, Produced: Set{}}
	return &Source{inner: inner, dict: dict, walker: newWalker(policy, dict), report: report}, report, nil
}

type rawGeneration struct {
	gen       collector.CollectedGeneration
	envelopes []facts.Envelope
}

// Next implements collector.Source.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if !s.drained {
		if err := s.drain(ctx); err != nil {
			return collector.CollectedGeneration{}, false, err
		}
	}
	if len(s.queue) == 0 {
		return collector.CollectedGeneration{}, false, nil
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	return next, true, nil
}

func (s *Source) drain(ctx context.Context) error {
	s.drained = true
	var raws []rawGeneration
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("recordpseudo: %w", err)
		}
		gen, ok, err := s.inner.Next(ctx)
		if err != nil {
			return fmt.Errorf("recordpseudo: inner source: %w", err)
		}
		if !ok {
			break
		}
		envelopes := make([]facts.Envelope, 0, gen.FactCount())
		for env := range gen.Facts {
			envelopes = append(envelopes, env)
		}
		if gen.FactStreamErr != nil {
			if err := gen.FactStreamErr(); err != nil {
				return fmt.Errorf("recordpseudo: fact stream for scope %q: %w", gen.Scope.ScopeID, err)
			}
		}
		raws = append(raws, rawGeneration{gen: gen, envelopes: envelopes})
	}
	for _, raw := range raws {
		s.learnGeneration(raw)
	}
	s.queue = make([]collector.CollectedGeneration, 0, len(raws))
	for _, raw := range raws {
		s.queue = append(s.queue, s.rewriteGeneration(raw))
	}
	s.fillReport(len(raws))
	return nil
}

func (s *Source) learnGeneration(raw rawGeneration) {
	for key, value := range raw.gen.Scope.Metadata {
		s.walker.learn(key, value, ClassUnknown)
	}
	for _, env := range raw.envelopes {
		s.walker.learn("", env.Payload, ClassUnknown)
	}
}

func (s *Source) rewriteGeneration(raw rawGeneration) collector.CollectedGeneration {
	sc := raw.gen.Scope
	sc.ScopeID = s.dict.substitute(sc.ScopeID)
	sc.PartitionKey = s.dict.substitute(sc.PartitionKey)
	if raw.gen.Scope.Metadata != nil {
		sc.Metadata = make(map[string]string, len(raw.gen.Scope.Metadata))
		for key, value := range raw.gen.Scope.Metadata {
			sc.Metadata[key] = s.dict.substitute(value)
		}
	}
	gen := raw.gen.Generation
	gen.ScopeID = sc.ScopeID
	out := make([]facts.Envelope, 0, len(raw.envelopes))
	for _, env := range raw.envelopes {
		next := env
		next.ScopeID = sc.ScopeID
		next.StableFactKey = s.dict.substitute(env.StableFactKey)
		next.SourceRef.ScopeID = sc.ScopeID
		next.SourceRef.FactKey = next.StableFactKey
		next.SourceRef.SourceRecordID = s.dict.substitute(env.SourceRef.SourceRecordID)
		next.SourceRef.SourceURI = s.dict.substitute(env.SourceRef.SourceURI)
		if env.Payload != nil {
			next.Payload, _ = s.walker.rewrite("payload", "", env.Payload, ClassUnknown).(map[string]any)
		}
		out = append(out, next)
	}
	s.report.Facts += len(out)
	return collector.FactsFromSlice(sc, gen, out)
}

func (s *Source) fillReport(scopes int) {
	s.report.Scopes = scopes
	s.report.Tokens = len(s.dict.entries)
	for class, n := range s.dict.learned {
		s.report.Learned[class] = n
	}
	s.report.IPv4Collisions = s.dict.ipCollisions
	s.report.OpaquePaths = sortedKeys(s.walker.opaque)
	s.report.UnclassifiedPaths = sortedKeys(s.walker.unclassified)
	for _, pseudonym := range s.dict.entries {
		s.report.Produced[pseudonym] = struct{}{}
	}
}

func sortedKeys(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for key := range counts {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
