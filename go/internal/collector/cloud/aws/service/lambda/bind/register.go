// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind binds the Lambda service scanner into the
// runtime registry. Importing this package for its init side effect adds
// the production scanner to the registry without modifying any shared file.
package bind

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/lambda"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/lambda/sdk"
)

func init() {
	runtime.Register(runtime.ScannerRegistration{
		ServiceKind:          aws.ServiceLambda,
		RequiresRedactionKey: true,
		Build: func(d runtime.ScannerDeps) (runtime.ServiceScanner, error) {
			if d.RedactionKey.IsZero() {
				return nil, fmt.Errorf("lambda scanner redaction key is required")
			}
			return svc.Scanner{
				Client:       sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
				RedactionKey: d.RedactionKey,
			}, nil
		},
	})
}
