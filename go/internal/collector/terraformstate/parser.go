package terraformstate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type resourceContext struct {
	Mode     string
	Type     string
	Name     string
	Module   string
	Provider string
}

type instanceContext struct {
	HasIndexKey  bool
	IndexKeyHash string
}

type snapshotMetadata struct {
	FormatVersion    string
	TerraformVersion string
	Serial           int64
	Lineage          string
}

type outputPayload struct {
	Sensitive bool
	Value     any
	HasScalar bool
	HasValue  bool
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
		decoder: decoder,
		options: options,
	}
	if err := parser.parse(); err != nil {
		return ParseResult{}, err
	}
	return ParseResult{Facts: parser.facts}, nil
}

type stateParser struct {
	decoder  *json.Decoder
	options  ParseOptions
	snapshot snapshotMetadata
	facts    []facts.Envelope
	warnings []warningPayload
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

func (p *stateParser) readOutputs() error {
	if err := readOpeningDelim(p.decoder, '{', "terraform state outputs"); err != nil {
		return err
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state output name: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state output name must be a string")
		}
		output, err := p.readOutput(name)
		if err != nil {
			return err
		}
		p.emitOutput(name, output)
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state outputs: %w", err)
	}
	return nil
}

func (p *stateParser) readOutput(name string) (outputPayload, error) {
	output := outputPayload{}
	if err := readOpeningDelim(p.decoder, '{', "terraform state output "+name); err != nil {
		return outputPayload{}, err
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return outputPayload{}, fmt.Errorf("read terraform state output %q key: %w", name, err)
		}
		key, ok := token.(string)
		if !ok {
			return outputPayload{}, fmt.Errorf("terraform state output %q key must be a string", name)
		}
		switch key {
		case "sensitive":
			if err := p.decoder.Decode(&output.Sensitive); err != nil {
				return outputPayload{}, fmt.Errorf("decode terraform state output %q sensitivity: %w", name, err)
			}
		case "value":
			value, scalar, err := readScalarOrSkip(p.decoder)
			if err != nil {
				return outputPayload{}, fmt.Errorf("decode terraform state output %q value: %w", name, err)
			}
			output.Value = value
			output.HasScalar = scalar
			output.HasValue = true
		default:
			if err := skipValue(p.decoder); err != nil {
				return outputPayload{}, err
			}
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return outputPayload{}, fmt.Errorf("close terraform state output %q: %w", name, err)
	}
	return output, nil
}

func (p *stateParser) emitOutput(name string, output outputPayload) {
	payload := map[string]any{
		"name":      name,
		"sensitive": output.Sensitive,
	}
	source := "outputs." + name
	if output.Sensitive {
		if output.HasScalar {
			payload["value"] = redactionMap(redact.Scalar(output.Value, "sensitive_output", source, p.options.RedactionKey))
		} else if output.HasValue {
			payload["value_shape"] = "composite"
			p.warnings = append(p.warnings, warningPayload{
				WarningKind: "output_value_dropped",
				Reason:      "sensitive_composite_output",
				Source:      source,
			})
		}
	} else if output.HasScalar {
		payload["value"] = output.Value
	} else if output.HasValue {
		payload["value_shape"] = "composite"
	}
	p.facts = append(p.facts, p.envelope(facts.TerraformStateOutputFactKind, "output:"+name, payload, name))
}

func (p *stateParser) readResources() error {
	if err := readOpeningDelim(p.decoder, '[', "terraform state resources"); err != nil {
		return err
	}
	for index := 0; p.decoder.More(); index++ {
		if err := p.readResource(index); err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resources: %w", err)
	}
	return nil
}

func (p *stateParser) readResource(resourceIndex int) error {
	resource := resourceContext{}
	sawInstances := false
	if err := readOpeningDelim(p.decoder, '{', fmt.Sprintf("terraform state resource %d", resourceIndex)); err != nil {
		return err
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state resource %d key: %w", resourceIndex, err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state resource %d key must be a string", resourceIndex)
		}
		switch key {
		case "mode":
			resource.Mode, err = readString(p.decoder, "terraform state resource mode")
		case "type":
			resource.Type, err = readString(p.decoder, "terraform state resource type")
		case "name":
			resource.Name, err = readString(p.decoder, "terraform state resource name")
		case "module":
			resource.Module, err = readString(p.decoder, "terraform state resource module")
		case "provider":
			resource.Provider, err = readString(p.decoder, "terraform state resource provider")
		case "instances":
			if err := validateResourceIdentity(resource); err != nil {
				return fmt.Errorf("terraform state resource %d identity before instances: %w", resourceIndex, err)
			}
			sawInstances = true
			err = p.readInstances(resource)
		default:
			err = skipValue(p.decoder)
		}
		if err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resource %d: %w", resourceIndex, err)
	}
	if !sawInstances {
		return p.emitResourceInstance(resource, instanceContext{}, 0, map[string]any{})
	}
	return nil
}

func (p *stateParser) readInstances(resource resourceContext) error {
	if err := readOpeningDelim(p.decoder, '[', "terraform state resource instances"); err != nil {
		return err
	}
	count := 0
	for ; p.decoder.More(); count++ {
		if err := p.readInstance(resource, count); err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resource instances: %w", err)
	}
	if count == 0 {
		return p.emitResourceInstance(resource, instanceContext{}, 0, map[string]any{})
	}
	return nil
}

func (p *stateParser) readInstance(resource resourceContext, instanceIndex int) error {
	instance := instanceContext{}
	attributes := []attributeValue{}
	if err := readOpeningDelim(p.decoder, '{', "terraform state resource instance"); err != nil {
		return err
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state resource instance key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state resource instance key must be a string")
		}
		switch key {
		case "index_key":
			value, scalar, err := readScalarOrSkip(p.decoder)
			if err != nil {
				return fmt.Errorf("decode terraform state instance index key: %w", err)
			}
			if scalar {
				instance.HasIndexKey = true
				instance.IndexKeyHash = instanceIndexHash(value)
			}
		case "attributes":
			readAttributes, err := readAttributeValues(p.decoder)
			if err != nil {
				return err
			}
			attributes = readAttributes
		default:
			if err := skipValue(p.decoder); err != nil {
				return err
			}
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resource instance: %w", err)
	}
	address := resourceAddress(resource, instance, instanceIndex)
	return p.emitResourceInstance(resource, instance, instanceIndex, p.classifyAttributes(address, attributes))
}

func (p *stateParser) emitResourceInstance(resource resourceContext, instance instanceContext, instanceIndex int, attributes map[string]any) error {
	if err := validateResourceIdentity(resource); err != nil {
		return err
	}
	address := resourceAddress(resource, instance, instanceIndex)
	payload := map[string]any{
		"address":    address,
		"mode":       strings.TrimSpace(resource.Mode),
		"type":       strings.TrimSpace(resource.Type),
		"name":       strings.TrimSpace(resource.Name),
		"module":     strings.TrimSpace(resource.Module),
		"provider":   strings.TrimSpace(resource.Provider),
		"attributes": attributes,
	}
	p.facts = append(p.facts, p.envelope(facts.TerraformStateResourceFactKind, "resource:"+address, payload, address))
	return nil
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
	p.facts = append([]facts.Envelope{
		p.envelope(facts.TerraformStateSnapshotFactKind, "snapshot", payload, locatorHash(p.options.Source)),
	}, p.facts...)
}

func (p *stateParser) emitWarnings() {
	sort.Slice(p.warnings, func(i, j int) bool {
		left := p.warnings[i]
		right := p.warnings[j]
		return left.WarningKind+"\x00"+left.Source+"\x00"+left.Reason < right.WarningKind+"\x00"+right.Source+"\x00"+right.Reason
	})
	for _, warning := range p.warnings {
		payload := map[string]any{
			"warning_kind": warning.WarningKind,
			"reason":       warning.Reason,
			"source":       warning.Source,
		}
		key := "warning:" + warning.WarningKind + ":" + warning.Source + ":" + warning.Reason
		p.facts = append(p.facts, p.envelope(facts.TerraformStateWarningFactKind, key, payload, warning.Source))
	}
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

type warningPayload struct {
	WarningKind string
	Reason      string
	Source      string
}
