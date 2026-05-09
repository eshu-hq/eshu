package terraformstate

import (
	"encoding/json"
	"fmt"
)

func readOpeningDelim(decoder *json.Decoder, want json.Delim, label string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read %s: %w", label, err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != want {
		return fmt.Errorf("%s must start with %q", label, want)
	}
	return nil
}

func readString(decoder *json.Decoder, label string) (string, error) {
	var value string
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode %s: %w", label, err)
	}
	return value, nil
}

func readScalarOrSkip(decoder *json.Decoder) (any, bool, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, false, err
	}
	if delim, ok := token.(json.Delim); ok {
		return nil, false, skipNested(decoder, delim)
	}
	return token, true, nil
}

func skipValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); ok {
		return skipNested(decoder, delim)
	}
	return nil
}

func skipNested(decoder *json.Decoder, opening json.Delim) error {
	switch opening {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := skipValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported json delimiter %q", opening)
	}
	_, err := decoder.Token()
	return err
}
