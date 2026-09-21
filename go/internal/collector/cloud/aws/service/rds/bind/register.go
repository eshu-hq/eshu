// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind binds the RDS service scanner into the
// runtime registry. Importing this package for its init side effect adds
// the production scanner to the registry without modifying any shared file.
package bind

import (
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/rds"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/rds/sdk"
)

func init() {
	runtime.Register(runtime.ScannerRegistration{
		ServiceKind: aws.ServiceRDS,
		Build: func(d runtime.ScannerDeps) (runtime.ServiceScanner, error) {
			return svc.Scanner{
				Client: sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
			}, nil
		},
	})
}
