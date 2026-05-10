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
	bodyFacts := &collectFactSink{}
	parser, err := parseState(ctx, reader, options, bodyFacts)
	if err != nil {
		return ParseResult{}, err
	}
	allFacts := &collectFactSink{}
	if err := emitParsedFacts(ctx, parser, bodyFacts.Replay, allFacts); err != nil {
		return ParseResult{}, err
	}
	return ParseResult{
		Facts:             allFacts.Facts(),
		ResourceFacts:     parser.resourceFacts,
		RedactionsApplied: parser.redactions,
	}, nil
}

// ParseStream streams a Terraform state reader into sink without retaining all
// emitted facts in parser memory. Sink receives facts only after the state
// identity has been validated; body facts are replayed in the same order Parse
// returns them.
func ParseStream(
	ctx context.Context,
	reader io.Reader,
	options ParseOptions,
	sink FactSink,
) (ParseStreamResult, error) {
	if sink == nil {
		return ParseStreamResult{}, fmt.Errorf("terraform state fact sink must not be nil")
	}
	bodyFacts, err := newFactSpool()
	if err != nil {
		return ParseStreamResult{}, err
	}
	defer bodyFacts.Close()

	parser, err := parseState(ctx, reader, options, bodyFacts)
	if err != nil {
		return ParseStreamResult{}, err
	}
	if err := emitParsedFacts(ctx, parser, bodyFacts.Replay, sink); err != nil {
		return ParseStreamResult{}, err
	}
	return ParseStreamResult{
		ResourceFacts:     parser.resourceFacts,
		RedactionsApplied: parser.redactions,
	}, nil
}

func parseState(ctx context.Context, reader io.Reader, options ParseOptions, bodyFacts FactSink) (stateParser, error) {
	if err := ctx.Err(); err != nil {
		return stateParser{}, err
	}
	if reader == nil {
		return stateParser{}, fmt.Errorf("terraform state reader must not be nil")
	}
	if bodyFacts == nil {
		return stateParser{}, fmt.Errorf("terraform state body fact sink must not be nil")
	}

	options = normalizedParseOptions(options)
	if err := options.Validate(); err != nil {
		return stateParser{}, err
	}

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	parser := stateParser{
		ctx:        ctx,
		decoder:    decoder,
		options:    options,
		bodyFacts:  bodyFacts,
		redactions: map[string]int64{},
	}
	if err := parser.addSourceWarnings(options.SourceWarnings); err != nil {
		return stateParser{}, err
	}
	if err := parser.parse(); err != nil {
		return stateParser{}, err
	}
	return parser, nil
}

func emitParsedFacts(
	ctx context.Context,
	parser stateParser,
	replayBodyFacts func(context.Context, FactSink) error,
	sink FactSink,
) error {
	if err := sink.Emit(ctx, parser.snapshotFact()); err != nil {
		return err
	}
	if err := replayBodyFacts(ctx, sink); err != nil {
		return err
	}
	return nil
}

type stateParser struct {
	ctx           context.Context
	decoder       *json.Decoder
	options       ParseOptions
	snapshot      snapshotMetadata
	bodyFacts     FactSink
	resourceFacts int64
	redactions    map[string]int64
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
		if err := p.checkContext(); err != nil {
			return err
		}
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
	if err := p.checkContext(); err != nil {
		return err
	}
	if err := expectEOF(p.decoder); err != nil {
		return err
	}
	if err := p.validateSnapshotIdentity(); err != nil {
		return err
	}
	return nil
}

func (p *stateParser) readField(key string) error {
	if err := p.checkContext(); err != nil {
		return err
	}
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

func (p *stateParser) emitBodyFact(envelope facts.Envelope) error {
	if err := p.checkContext(); err != nil {
		return err
	}
	return p.bodyFacts.Emit(p.ctx, envelope)
}

func (p *stateParser) checkContext() error {
	if p.ctx == nil {
		return nil
	}
	return p.ctx.Err()
}

func (p *stateParser) snapshotFact() facts.Envelope {
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
	return p.envelope(facts.TerraformStateSnapshotFactKind, "snapshot", payload, locatorHash(p.options.Source))
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
