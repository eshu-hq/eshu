// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureAWSRelationshipDecodeFile = `package relationships

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func decodeAWSRelationship(env facts.Envelope) (awsv1.Relationship, error) {
	relationship, err := factschema.DecodeAWSRelationship(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Relationship{}, newFactDecodeError(factschema.FactKindAWSRelationship, err)
	}
	return relationship, nil
}
`

const fixtureAWSSecurityGroupRuleDecodeFile = `package relationships

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func decodeAWSSecurityGroupRule(env facts.Envelope) (awsv1.SecurityGroupRule, error) {
	rule, err := factschema.DecodeAWSSecurityGroupRule(factschemaEnvelope(env))
	if err != nil {
		return awsv1.SecurityGroupRule{}, newFactDecodeError(factschema.FactKindAWSSecurityGroupRule, err)
	}
	return rule, nil
}
`

const fixtureAWSIAMPrincipalDecodeFile = `package replay

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

func decodeAWSIAMPrincipal(env facts.Envelope) (iamv1.Principal, error) {
	principal, err := factschema.DecodeAWSIAMPrincipal(factschemaEnvelope(env))
	if err != nil {
		return iamv1.Principal{}, newFactDecodeError(factschema.FactKindAWSIAMPrincipal, err)
	}
	return principal, nil
}
`

func TestLoadCoversLoaderRelationshipsAndReplayDirs(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	reducerDir := writeDecodeSurface(t, fixtureDecodeFile, `package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

func reduce(env facts.Envelope) string {
	resource, _ := decodeAWSResource(env)
	return resource.ResourceID
}
`)
	loaderDir := writeDecodeSurface(t, fixtureAWSRelationshipDecodeFile, `package loader

import "github.com/eshu-hq/eshu/go/internal/facts"

func load(env facts.Envelope) string {
	relationship, _ := decodeAWSRelationship(env)
	return relationship.SourceResourceID
}
`)
	relationshipsDir := writeDecodeSurface(t, fixtureAWSSecurityGroupRuleDecodeFile, `package relationships

import "github.com/eshu-hq/eshu/go/internal/facts"

func relate(env facts.Envelope) string {
	rule, _ := decodeAWSSecurityGroupRule(env)
	return rule.GroupID
}
`)
	replayDir := writeDecodeSurface(t, fixtureAWSIAMPrincipalDecodeFile, `package replay

import "github.com/eshu-hq/eshu/go/internal/facts"

func replay(env facts.Envelope) string {
	principal, _ := decodeAWSIAMPrincipal(env)
	return principal.PrincipalARN
}
`)

	manifest, err := Load(Paths{
		RepoRoot:         root,
		ReducerDir:       reducerDir,
		ProjectorDir:     t.TempDir(),
		QueryDir:         t.TempDir(),
		LoaderDir:        loaderDir,
		RelationshipsDir: relationshipsDir,
		ReplayDir:        replayDir,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertManifestUsage(t, manifest, "FactKindAWSResource", "ResourceID", "consumer.go")
	assertManifestUsage(t, manifest, "FactKindAWSRelationship", "SourceResourceID", "consumer.go")
	assertManifestUsage(t, manifest, "FactKindAWSSecurityGroupRule", "GroupID", "consumer.go")
	assertManifestUsage(t, manifest, "FactKindAWSIAMPrincipal", "PrincipalARN", "consumer.go")
}

func writeDecodeSurface(t *testing.T, decodeFile string, consumerFile string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "factschema_decode.go"), []byte(decodeFile), 0o600); err != nil {
		t.Fatalf("write decode fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "consumer.go"), []byte(consumerFile), 0o600); err != nil {
		t.Fatalf("write consumer fixture: %v", err)
	}
	return dir
}

func assertManifestUsage(t *testing.T, manifest Manifest, factKind string, goField string, file string) {
	t.Helper()
	for _, kind := range manifest.Kinds {
		if kind.FactKind != factKind {
			continue
		}
		for _, used := range kind.UsedFields {
			if used.GoName != goField {
				continue
			}
			for _, usedFile := range used.Files {
				if usedFile == file {
					return
				}
			}
			t.Fatalf("%s.%s files = %v, want %s", factKind, goField, used.Files, file)
		}
		t.Fatalf("%s used fields = %+v, want %s", factKind, kind.UsedFields, goField)
	}
	t.Fatalf("%s not found in manifest: %+v", factKind, manifest.Kinds)
}
