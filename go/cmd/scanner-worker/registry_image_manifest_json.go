// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	maxScannerManifestBytes = 1 << 20
	maxManifestJSONDepth    = 64
	maxManifestJSONTokens   = 100_000
)

// validateUniqueManifestJSONKeys rejects ambiguous JSON before descriptor
// interpretation. The manifest body is already capped at one MiB by transport.
func validateUniqueManifestJSONKeys(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) > maxScannerManifestBytes {
		return errors.New("OCI manifest JSON byte limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tokens int
	if err := walkManifestJSON(ctx, decoder, 0, &tokens); err != nil {
		return err
	}
	_, err := nextManifestJSONToken(ctx, decoder, &tokens)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("multiple OCI manifest JSON values")
}

func walkManifestJSON(ctx context.Context, decoder *json.Decoder, depth int, tokens *int) error {
	if depth > maxManifestJSONDepth {
		return errors.New("OCI manifest JSON depth exceeded")
	}
	token, err := nextManifestJSONToken(ctx, decoder, tokens)
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := nextManifestJSONToken(ctx, decoder, tokens)
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid OCI manifest JSON key")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate OCI manifest JSON key")
			}
			seen[key] = struct{}{}
			if err := walkManifestJSON(ctx, decoder, depth+1, tokens); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkManifestJSON(ctx, decoder, depth+1, tokens); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid OCI manifest JSON delimiter")
	}
	closing, err := nextManifestJSONToken(ctx, decoder, tokens)
	if err != nil {
		return err
	}
	if (delimiter == '{' && closing != json.Delim('}')) || (delimiter == '[' && closing != json.Delim(']')) {
		return errors.New("invalid OCI manifest JSON closure")
	}
	return nil
}

func nextManifestJSONToken(ctx context.Context, decoder *json.Decoder, tokens *int) (json.Token, error) {
	*tokens = *tokens + 1
	if *tokens > maxManifestJSONTokens {
		return nil, errors.New("OCI manifest JSON token limit exceeded")
	}
	if *tokens%256 == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("parse OCI manifest JSON token: %w", err)
	}
	return token, nil
}
