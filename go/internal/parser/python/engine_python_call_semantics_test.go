// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonEmitsDottedCallMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "dotted_calls.py")
	writeTestFile(
		t,
		filePath,
		`class Client:
    def service(self):
        return self

client = Client()
client.service.request()
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := parsertest.AssertBucketItemByName(t, got, "function_calls", "request")
	parsertest.AssertStringFieldValue(t, call, "full_name", "client.service.request")
}

func TestDefaultEngineParsePathPythonEmitsMethodContextAndInferredReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.py")
	writeTestFile(
		t,
		filePath,
		`class LogProcessor:
    def create_partition(self, partition):
        return partition

class LogPartition:
    @staticmethod
    def from_event(source, target):
        return LogPartition()

def lambda_handler(event, context):
    log_processor = LogProcessor()
    partition = LogPartition.from_event(source=event, target="bucket")
    log_processor.create_partition(partition=partition)
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "create_partition"), "class_context", "LogProcessor")
	parsertest.AssertStringFieldValue(t, parsertest.AssertBucketItemByName(t, got, "functions", "from_event"), "class_context", "LogPartition")
	staticCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "LogPartition.from_event")
	parsertest.AssertStringFieldValue(t, staticCall, "name", "from_event")
	memberCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "log_processor.create_partition")
	parsertest.AssertStringFieldValue(t, memberCall, "name", "create_partition")
	parsertest.AssertStringFieldValue(t, memberCall, "inferred_obj_type", "LogProcessor")
}

func TestDefaultEngineParsePathPythonInfersSelfReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "processor.py")
	writeTestFile(
		t,
		filePath,
		`class LogProcessor:
    def object(self, bucket, key):
        return bucket, key

    def create_partition(self, partition):
        return self.object(bucket=partition.bucket, key=partition.key)
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	memberCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "self.object")
	parsertest.AssertStringFieldValue(t, memberCall, "name", "object")
	parsertest.AssertStringFieldValue(t, memberCall, "inferred_obj_type", "LogProcessor")
}
