package terraformstate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type snapshotMetadata struct {
	FormatVersion    string
	TerraformVersion string
	Serial           int64
	Lineage          string
}

// Parse streams a Terraform state reader into redacted fact envelopes.
func Parse(ctx context.Context, reader io.Reader, options ParseOptions) (ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return ParseResult{}, err
	}
	if reader == nil {
		return ParseResult{}, fmt.Errorf("terraform state reader must not be nil")
	}

	options = normalizedParseOptions(options)
	if err := options.Validate(); err != nil {
		return ParseResult{}, err
	}

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	parser := stateParser{
		decoder:    decoder,
		options:    options,
		redactions: map[string]int64{},
	}
	parser.addSourceWarnings(options.SourceWarnings)
	if err := parser.parse(); err != nil {
		return ParseResult{}, err
	}
	return ParseResult{
		Facts:             parser.facts,
		ResourceFacts:     parser.resourceFacts,
		RedactionsApplied: parser.redactions,
	}, nil
}

type stateParser struct {
	decoder          *json.Decoder
	options          ParseOptions
	snapshot         snapshotMetadata
	facts            []facts.Envelope
	warnings         []warningPayload
	modules          map[string]moduleObservation
	providerBindings map[string]providerBinding
	resourceFacts    int64
	redactions       map[string]int64
}

func (p *stateParser) parse() error {
	token, err := p.decoder.Token()
	if err != nil {
		return fmt.Errorf("read terraform state object: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("terraform state root must be an object")
	}

	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state object key must be a string")
		}
		if err := p.readField(key); err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state object: %w", err)
	}
	if err := expectEOF(p.decoder); err != nil {
		return err
	}
	if err := p.validateSnapshotIdentity(); err != nil {
		return err
	}

	p.emitSnapshot()
	p.emitModules()
	p.emitProviderBindings()
	p.emitWarnings()
	return nil
}

func (p *stateParser) readField(key string) error {
	switch key {
	case "format_version":
		return p.decoder.Decode(&p.snapshot.FormatVersion)
	case "terraform_version":
		return p.decoder.Decode(&p.snapshot.TerraformVersion)
	case "serial":
		return p.decoder.Decode(&p.snapshot.Serial)
	case "lineage":
		return p.decoder.Decode(&p.snapshot.Lineage)
	case "outputs":
		return p.readOutputs()
	case "resources":
		return p.readResources()
	default:
		return skipValue(p.decoder)
	}
}

func (p *stateParser) emitSnapshot() {
	payload := map[string]any{
		"format_version":    p.snapshot.FormatVersion,
		"terraform_version": p.snapshot.TerraformVersion,
		"serial":            p.snapshot.Serial,
		"lineage":           p.snapshot.Lineage,
		"backend_kind":      string(p.options.Source.BackendKind),
		"locator_hash":      locatorHash(p.options.Source),
		"source_size_bytes": p.options.Metadata.Size,
	}
	if strings.TrimSpace(p.options.Metadata.ETag) != "" {
		payload["etag"] = p.options.Metadata.ETag
	}
	p.facts = append([]facts.Envelope{
		p.envelope(facts.TerraformStateSnapshotFactKind, "snapshot", payload, locatorHash(p.options.Source)),
	}, p.facts...)
}

func (p *stateParser) validateSnapshotIdentity() error {
	lineage, serial, err := expectedSnapshotIdentity(p.options.Generation.FreshnessHint)
	if err != nil {
		return err
	}
	if p.snapshot.Lineage != lineage {
		return fmt.Errorf("terraform state lineage %q does not match generation lineage %q", p.snapshot.Lineage, lineage)
	}
	if p.snapshot.Serial != serial {
		return fmt.Errorf("terraform state serial %d does not match generation serial %d", p.snapshot.Serial, serial)
	}
	return nil
}

func (p *stateParser) envelope(kind string, stableKey string, payload map[string]any, sourceRecordID string) facts.Envelope {
	version, _ := facts.TerraformStateSchemaVersion(kind)
	key := kind + ":" + stableKey
	return facts.Envelope{
		FactID: facts.StableID("TerraformStateFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      p.options.Scope.ScopeID,
			"generation_id": p.options.Generation.GenerationID,
		}),
		ScopeID:          p.options.Scope.ScopeID,
		GenerationID:     p.options.Generation.GenerationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorTerraformState),
		FencingToken:     p.options.FencingToken,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       p.options.ObservedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorTerraformState),
			ScopeID:        p.options.Scope.ScopeID,
			GenerationID:   p.options.Generation.GenerationID,
			FactKey:        key,
			SourceURI:      sourceURI(p.options.Source),
			SourceRecordID: sourceRecordID,
		},
	}
}
