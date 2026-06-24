// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// parseGoSourceForSDKTest writes source to a temp file and runs the Go parser,
// returning the emitted payload. The parser reads from a path, so the fixture
// must live on disk.
func parseGoSourceForSDKTest(t *testing.T, source string) map[string]any {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v", err)
	}
	payload, err := Parse(parser, path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return payload
}

// receiverSDKServiceForCall returns the receiver_sdk_service field recorded for
// the first function_call whose name matches method, or "" with ok=false.
func receiverSDKServiceForCall(t *testing.T, payload map[string]any, method string) (string, bool) {
	t.Helper()

	calls, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls bucket missing or wrong type: %T", payload["function_calls"])
	}
	for _, call := range calls {
		if call["name"] != method {
			continue
		}
		if service, present := call["receiver_sdk_service"]; present {
			str, isString := service.(string)
			if !isString {
				t.Fatalf("receiver_sdk_service for %q is %T, want string", method, service)
			}
			return str, true
		}
		return "", false
	}
	t.Fatalf("no function_call named %q found", method)
	return "", false
}

func TestGoReceiverSDKServiceBindsV2Constructor(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func run(ctx context.Context, cfg interface{}) {
	client := s3.NewFromConfig(cfg)
	client.PutObject(ctx, nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)
	got, ok := receiverSDKServiceForCall(t, payload, "PutObject")
	if !ok {
		t.Fatalf("PutObject call missing receiver_sdk_service")
	}
	if got != "s3" {
		t.Fatalf("receiver_sdk_service = %q, want %q", got, "s3")
	}
}

func TestGoReceiverSDKServiceBindsTwoServicesWithoutCrossBinding(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func run(ctx context.Context, cfg interface{}) {
	storage := s3.NewFromConfig(cfg)
	table := dynamodb.NewFromConfig(cfg)
	storage.PutObject(ctx, nil)
	table.GetItem(ctx, nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)

	if got, ok := receiverSDKServiceForCall(t, payload, "PutObject"); !ok || got != "s3" {
		t.Fatalf("PutObject receiver_sdk_service = (%q, %v), want (%q, true)", got, ok, "s3")
	}
	if got, ok := receiverSDKServiceForCall(t, payload, "GetItem"); !ok || got != "dynamodb" {
		t.Fatalf("GetItem receiver_sdk_service = (%q, %v), want (%q, true)", got, ok, "dynamodb")
	}
}

func TestGoReceiverSDKServiceRespectsImportAlias(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	mys3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func run(sess interface{}) {
	c := mys3.New(sess)
	c.GetObject(nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)
	got, ok := receiverSDKServiceForCall(t, payload, "GetObject")
	if !ok || got != "s3" {
		t.Fatalf("GetObject receiver_sdk_service = (%q, %v), want (%q, true)", got, ok, "s3")
	}
}

func TestGoReceiverSDKServiceV1NewIdiom(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"github.com/aws/aws-sdk-go/service/s3"
)

func run(sess interface{}) {
	svc := s3.New(sess)
	svc.PutObject(nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)
	got, ok := receiverSDKServiceForCall(t, payload, "PutObject")
	if !ok || got != "s3" {
		t.Fatalf("PutObject receiver_sdk_service = (%q, %v), want (%q, true)", got, ok, "s3")
	}
}

func TestGoReceiverSDKServiceAbsentForNonSDKPackage(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"example.com/notaws/s3"
)

func run(cfg interface{}) {
	client := s3.NewFromConfig(cfg)
	client.PutObject(nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)
	if got, ok := receiverSDKServiceForCall(t, payload, "PutObject"); ok {
		t.Fatalf("PutObject unexpectedly bound receiver_sdk_service = %q for non-SDK package", got)
	}
}

func TestGoReceiverSDKServiceAbsentForAmbiguousReassignment(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func run(ctx context.Context, cfg interface{}, flag bool) {
	client := s3.NewFromConfig(cfg)
	client = dynamodb.NewFromConfig(cfg)
	client.PutObject(ctx, nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)
	if got, ok := receiverSDKServiceForCall(t, payload, "PutObject"); ok {
		t.Fatalf("PutObject unexpectedly bound receiver_sdk_service = %q under ambiguous reassignment", got)
	}
}

func TestGoReceiverSDKServiceAbsentForBareVarDeclaration(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func run() {
	var client *s3.Client
	client.PutObject(nil)
}
`
	payload := parseGoSourceForSDKTest(t, source)
	if got, ok := receiverSDKServiceForCall(t, payload, "PutObject"); ok {
		t.Fatalf("PutObject unexpectedly bound receiver_sdk_service = %q for bare var declaration", got)
	}
}
