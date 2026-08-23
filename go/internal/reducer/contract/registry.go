// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// OwnershipShape records whether a reducer domain owns cross-source and
// cross-scope reconciliation and how it produces canonical truth.
type OwnershipShape struct {
	CrossSource bool
	CrossScope  bool
	// CanonicalWrite marks a domain that persists canonical reducer-owned truth,
	// either as durable fact rows or as graph nodes/edges.
	CanonicalWrite bool
	// CounterEmit marks a domain whose canonical truth contract is satisfied
	// by emitting bounded metric counters and structured logs rather than
	// writing canonical graph nodes. At least one of CanonicalWrite or
	// CounterEmit must be true; both may be true.
	CounterEmit bool
}

// Validate checks that the ownership shape matches the reducer boundary.
func (o OwnershipShape) Validate() error {
	if !o.CrossSource {
		return errors.New("reducers must be cross-source")
	}
	if !o.CrossScope {
		return errors.New("reducers must be cross-scope")
	}
	if !o.CanonicalWrite && !o.CounterEmit {
		return errors.New("reducers must declare CanonicalWrite or CounterEmit")
	}
	return nil
}

// CrossScopeDependency declares canonical producer domains read across scopes.
type CrossScopeDependency struct {
	// ProducerDomains are the reducer domains whose canonical output this
	// consumer reads across scopes.
	ProducerDomains []Domain
}

// Validate checks that the dependency names known producer domains.
func (d CrossScopeDependency) Validate() error {
	if len(d.ProducerDomains) == 0 {
		return errors.New("cross-scope dependency must name at least one producer domain")
	}
	for _, producer := range d.ProducerDomains {
		if err := producer.Validate(); err != nil {
			return fmt.Errorf("cross-scope dependency producer %q: %w", producer, err)
		}
	}
	return nil
}

// DomainDefinition describes one reducer domain and its ownership shape.
type DomainDefinition struct {
	Domain        Domain
	Summary       string
	Ownership     OwnershipShape
	TruthContract truth.Contract
	Handler       Handler
	// CrossScopeDependencies declares the producer domains this domain reads
	// across ingestion scopes.
	CrossScopeDependencies []CrossScopeDependency
}

// Validate checks the domain definition for registration.
func (d DomainDefinition) Validate() error {
	if err := d.Domain.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(d.Summary) == "" {
		return errors.New("summary must not be blank")
	}
	if err := d.Ownership.Validate(); err != nil {
		return err
	}
	if err := d.TruthContract.Validate(); err != nil {
		return err
	}
	for _, dependency := range d.CrossScopeDependencies {
		if err := dependency.Validate(); err != nil {
			return fmt.Errorf("domain %q cross-scope dependency: %w", d.Domain, err)
		}
	}
	return nil
}

// Handler executes one reducer intent for a registered domain.
type Handler interface {
	Handle(context.Context, Intent) (Result, error)
}

// HandlerFunc adapts a function into a Handler.
type HandlerFunc func(context.Context, Intent) (Result, error)

// Handle executes the wrapped function.
func (f HandlerFunc) Handle(ctx context.Context, intent Intent) (Result, error) {
	return f(ctx, intent)
}
