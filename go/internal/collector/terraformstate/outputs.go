package terraformstate

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

type outputPayload struct {
	Sensitive bool
	Value     any
	HasScalar bool
	HasValue  bool
}

func (p *stateParser) readOutputs() error {
	if err := readOpeningDelim(p.decoder, '{', "terraform state outputs"); err != nil {
		return err
	}
	for p.decoder.More() {
		if err := p.checkContext(); err != nil {
			return err
		}
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
		if err := p.emitOutput(name, output); err != nil {
			return err
		}
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
		if err := p.checkContext(); err != nil {
			return outputPayload{}, err
		}
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

func (p *stateParser) emitOutput(name string, output outputPayload) error {
	payload := map[string]any{
		"name":      name,
		"sensitive": output.Sensitive,
	}
	source := "outputs." + name
	if output.Sensitive {
		if output.HasScalar {
			reason := "sensitive_output"
			payload["value"] = redactionMap(redact.Scalar(output.Value, reason, source, p.options.RedactionKey))
			p.recordRedaction(reason)
		} else if output.HasValue {
			payload["value_shape"] = "composite"
			p.recordRedaction("sensitive_composite_output")
			if err := p.emitWarning(warningPayload{
				WarningKind: "output_value_dropped",
				Reason:      "sensitive_composite_output",
				Source:      source,
			}); err != nil {
				return err
			}
		}
	} else if output.HasScalar {
		payload["value"] = output.Value
	} else if output.HasValue {
		payload["value_shape"] = "composite"
	}
	return p.emitBodyFact(p.envelope(facts.TerraformStateOutputFactKind, "output:"+name, payload, name))
}
