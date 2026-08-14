// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evbundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

// ExportDemo builds the deterministic fixture bundle, validates it, stamps
// the passing validation, and returns the rendered JSON.
//
// The scopeID is a caller-supplied string that lands in Identity.ScopeID and
// in every reproduce call's repo_id argument, so it is content, not just a
// label. Validate runs over the marshalled bundle before anything is
// returned, which is what stops a scope handle carrying a path, an endpoint,
// or a credential from reaching a shared artifact.
func ExportDemo(scopeID string) ([]byte, error) {
	bundle := evidencebundle.BuildDemoBundle(evidencebundle.DemoBundleOptions{ScopeID: scopeID})
	if err := evidencebundle.Validate(bundle); err != nil {
		return nil, fmt.Errorf("validate generated evidence bundle: %w", err)
	}
	// Stamp only now that Validate actually returned nil.
	bundle = evidencebundle.StampValidation(bundle)
	return renderBundle(bundle)
}

// ExportLive fetches the three status routes through fetcher, composes a live
// bundle from them, validates it, stamps the passing validation, and returns
// the rendered JSON.
//
// An empty scopeID makes evidencebundle.BuildLiveBundle use its stack-wide
// "live:local" identity, which is the only honest label for evidence read
// from stack-global routes. createdAt stamps Identity.CreatedAt; the composer
// never calls time.Now itself, so the caller owns the clock.
func ExportLive(fetcher StatusFetcher, scopeID string, createdAt time.Time) ([]byte, error) {
	snapshot, err := FetchLiveSnapshot(fetcher)
	if err != nil {
		return nil, err
	}
	bundle := evidencebundle.BuildLiveBundle(snapshot, evidencebundle.LiveBundleOptions{
		ScopeID:   scopeID,
		CreatedAt: createdAt,
	})
	if err := evidencebundle.Validate(bundle); err != nil {
		return nil, fmt.Errorf("validate live evidence bundle: %w", err)
	}
	// Stamp only now that Validate actually returned nil.
	bundle = evidencebundle.StampValidation(bundle)
	return renderBundle(bundle)
}

// renderBundle serializes a validated bundle.
//
// evidencebundle.RenderJSON's error is returned unchanged so the operator
// sees the encoder's own message rather than a second layer naming a step
// they did not ask for.
//
//nolint:wrapcheck // preserves the existing operator-visible message verbatim
func renderBundle(bundle evidencebundle.Bundle) ([]byte, error) {
	return evidencebundle.RenderJSON(bundle)
}

// WriteBundle writes rendered bundle JSON to outPath when it is set, and to
// out otherwise. The file is created 0600: a bundle is share-safe by
// construction, but it is the operator's decision who reads it, not the
// filesystem's default umask.
func WriteBundle(out io.Writer, raw []byte, outPath string) error {
	if strings.TrimSpace(outPath) != "" {
		if err := os.WriteFile(outPath, raw, 0o600); err != nil {
			return fmt.Errorf("write evidence bundle: %w", err)
		}
		return nil
	}
	if _, err := out.Write(raw); err != nil {
		return fmt.Errorf("write evidence bundle: %w", err)
	}
	return nil
}

// ReadBundleInput reads a bundle to validate from path, or from in when no
// path is given.
func ReadBundleInput(in io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local validation path, not an HTTP request param //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read evidence bundle: %w", err)
		}
		return raw, nil
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("read evidence bundle stdin: %w", err)
	}
	return raw, nil
}

// ValidateBundle decodes raw as an evidence bundle, runs the full validation,
// writes the one-line verdict to out, and returns the validation error.
//
// The verdict line is written on both outcomes so an operator piping the
// command sees a result either way; the returned error carries the reason.
// evidencebundle.Validate's error is returned unchanged because it already
// names the rule that rejected the bundle.
//
//nolint:wrapcheck // preserves the existing operator-visible message verbatim
func ValidateBundle(out io.Writer, raw []byte) error {
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return fmt.Errorf("decode evidence bundle: %w", err)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		_, _ = fmt.Fprintln(out, "evidence bundle validation: failed")
		return err
	}
	_, _ = fmt.Fprintln(out, "evidence bundle validation: passed")
	return nil
}
