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

// fixtureAzureDecodeFile is a second per-family decode-seam file so the glob
// test proves seams merge across factschema_decode.go and
// factschema_decode_<family>.go — the case that made azure coverage possible.
const fixtureAzureDecodeFile = `package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

func decodeAzureCloudResource(env facts.Envelope) (azurev1.CloudResource, error) {
	resource, err := factschema.DecodeAzureCloudResource(factschemaEnvelope(env))
	if err != nil {
		return azurev1.CloudResource{}, newFactDecodeError(factschema.FactKindAzureCloudResource, err)
	}
	return resource, nil
}
`

func TestParseDecodeSeamsGlobMergesPerFamilyFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "factschema_decode.go"), []byte(fixtureDecodeFile), 0o600); err != nil {
		t.Fatalf("write base fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "factschema_decode_azure.go"), []byte(fixtureAzureDecodeFile), 0o600); err != nil {
		t.Fatalf("write azure fixture: %v", err)
	}

	seams, err := ParseDecodeSeamsGlob(filepath.Join(dir, "factschema_decode*.go"))
	if err != nil {
		t.Fatalf("ParseDecodeSeamsGlob() error = %v", err)
	}

	byName := map[string]DecodeSeam{}
	for _, s := range seams {
		byName[s.FuncName] = s
	}
	// The base file's two seams PLUS the azure file's one seam must all appear:
	// this is the exact behavior the single-file parse missed.
	for _, want := range []string{"decodeAWSResource", "decodeAWSIAMPrincipal", "decodeAzureCloudResource"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("seam %q missing from glob merge; per-family decode file was not scanned: %+v", want, seams)
		}
	}
	if azure := byName["decodeAzureCloudResource"]; azure.QualifiedStruct() != "azurev1.CloudResource" {
		t.Errorf("decodeAzureCloudResource QualifiedStruct() = %q, want azurev1.CloudResource", azure.QualifiedStruct())
	}
}

func TestParseDecodeSeamsGlobRejectsDuplicateFuncName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Two files each declaring decodeAWSResource: a real duplication bug the
	// gate must surface, not silently deduplicate.
	if err := os.WriteFile(filepath.Join(dir, "factschema_decode.go"), []byte(fixtureDecodeFile), 0o600); err != nil {
		t.Fatalf("write base fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "factschema_decode_dup.go"), []byte(fixtureDecodeFile), 0o600); err != nil {
		t.Fatalf("write dup fixture: %v", err)
	}

	_, err := ParseDecodeSeamsGlob(filepath.Join(dir, "factschema_decode*.go"))
	if err == nil {
		t.Fatal("ParseDecodeSeamsGlob() error = nil, want a duplicate-func-name error across two files")
	}
}
