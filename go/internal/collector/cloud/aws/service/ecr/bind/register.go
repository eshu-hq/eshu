// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind binds the ECR service scanner into the
// runtime registry. Importing this package for its init side effect adds
// the production scanner to the registry without modifying any shared file.
package bind

import (
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/ecr"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/ecr/sdk"
)

func init() {
	runtime.Register(runtime.ScannerRegistration{
		ServiceKind: aws.ServiceECR,
		Build: func(d runtime.ScannerDeps) (runtime.ServiceScanner, error) {
			return svc.Scanner{
				Client: sdkadapter.NewClientWithCheckpoints(
					d.AWSConfig,
					d.Boundary,
					d.Tracer,
					d.Instruments,
					d.Checkpoints,
				),
			}, nil
		},
	})
}
