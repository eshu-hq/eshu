// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticbeanstalk/runtimebind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestElasticBeanstalkRuntimeBindRegisters confirms importing the binding
// installs the Elastic Beanstalk scanner builder and that it declares the
// redaction-key requirement so config validation can derive it from the
// registry.
func TestElasticBeanstalkRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceElasticBeanstalk)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceElasticBeanstalk)
	}
	if !awsruntime.ServiceRequiresRedactionKey(awscloud.ServiceElasticBeanstalk) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = false, want true", awscloud.ServiceElasticBeanstalk)
	}
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig:    aws.Config{Region: "us-east-1"},
		Boundary:     awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceElasticBeanstalk},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestElasticBeanstalkRuntimeBindRequiresRedactionKey covers the fail-closed
// guard: the builder rejects a zero redaction key.
func TestElasticBeanstalkRuntimeBindRequiresRedactionKey(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceElasticBeanstalk)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceElasticBeanstalk)
	}
	_, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceElasticBeanstalk},
	})
	if err == nil {
		t.Fatalf("build() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("build() error = %q, want redaction key", err)
	}
}
