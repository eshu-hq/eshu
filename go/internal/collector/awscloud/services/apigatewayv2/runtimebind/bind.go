// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind binds the API Gateway v2 service scanner into the
// awsruntime registry. Importing this package for its init side effect adds the
// production scanner to the registry without modifying any shared file.
package runtimebind

import (
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	svc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigatewayv2"
	sdkadapter "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigatewayv2/awssdk"
)

func init() {
	awsruntime.Register(awsruntime.ScannerRegistration{
		ServiceKind: awscloud.ServiceAPIGatewayV2,
		Build: func(d awsruntime.ScannerDeps) (awsruntime.ServiceScanner, error) {
			return svc.Scanner{
				Client: sdkadapter.NewClient(d.AWSConfig, d.Boundary, d.Tracer, d.Instruments),
			}, nil
		},
	})
}
