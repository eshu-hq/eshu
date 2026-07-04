// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureDecodeFile = `package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Resource{}, newFactDecodeError(factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

func decodeAWSIAMPrincipal(env facts.Envelope) (iamv1.Principal, error) {
	principal, err := factschema.DecodeAWSIAMPrincipal(factschemaEnvelope(env))
	if err != nil {
		return iamv1.Principal{}, newFactDecodeError(factschema.FactKindAWSIAMPrincipal, err)
	}
	return principal, nil
}

// notASeam has the right name prefix but the wrong shape (extra return
// value) and must not be misidentified as a decode seam.
func decodeNotASeam(env facts.Envelope) (awsv1.Resource, string, error) {
	return awsv1.Resource{}, "", nil
}

// helperNoFactKind returns the right shape but never references a
// factschema.FactKind* constant, so it has no attributable wire fact kind
// and must be excluded.
func helperNoFactKind(env facts.Envelope) (awsv1.Resource, error) {
	return awsv1.Resource{}, nil
}
`

func writeFixtureFile(t *testing.T, name, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

func TestParseDecodeSeams(t *testing.T) {
	t.Parallel()

	path := writeFixtureFile(t, "factschema_decode.go", fixtureDecodeFile)
	seams, err := ParseDecodeSeams(path)
	if err != nil {
		t.Fatalf("ParseDecodeSeams() error = %v", err)
	}

	if len(seams) != 2 {
		t.Fatalf("len(seams) = %d, want 2 (decodeAWSResource, decodeAWSIAMPrincipal); got %+v", len(seams), seams)
	}

	byName := map[string]DecodeSeam{}
	for _, s := range seams {
		byName[s.FuncName] = s
	}

	resource, ok := byName["decodeAWSResource"]
	if !ok {
		t.Fatal("decodeAWSResource not found in parsed seams")
	}
	if resource.FactKindConst != "FactKindAWSResource" {
		t.Errorf("FactKindConst = %q, want FactKindAWSResource", resource.FactKindConst)
	}
	if resource.QualifiedStruct() != "awsv1.Resource" {
		t.Errorf("QualifiedStruct() = %q, want awsv1.Resource", resource.QualifiedStruct())
	}

	principal, ok := byName["decodeAWSIAMPrincipal"]
	if !ok {
		t.Fatal("decodeAWSIAMPrincipal not found in parsed seams")
	}
	if principal.QualifiedStruct() != "iamv1.Principal" {
		t.Errorf("QualifiedStruct() = %q, want iamv1.Principal", principal.QualifiedStruct())
	}

	if _, ok := byName["decodeNotASeam"]; ok {
		t.Error("decodeNotASeam was misidentified as a decode seam; its extra return value must exclude it")
	}
	if _, ok := byName["helperNoFactKind"]; ok {
		t.Error("helperNoFactKind was misidentified as a decode seam; it never references a factschema.FactKind* constant")
	}
}

func TestParseDecodeSeamsMissingFileErrors(t *testing.T) {
	t.Parallel()

	_, err := ParseDecodeSeams(filepath.Join(t.TempDir(), "does_not_exist.go"))
	if err == nil {
		t.Fatal("ParseDecodeSeams() error = nil, want an error for a missing file")
	}
}
