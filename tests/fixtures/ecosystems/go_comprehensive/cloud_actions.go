// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// cloud_actions.go exercises AWS SDK v2 call-site detection (rc-10 fixture).
//
// The Go parser's import-binding analysis detects the receiver variable bound to
// s3.NewFromConfig and records receiver_sdk_service = "s3" on every method call
// via that receiver. The reducer's buildInvokesCloudActionIntentRows resolves
// (service="s3", method="PutObject") → "s3:putobject" via the explicit mapping
// table and emits a Function-[:INVOKES_CLOUD_ACTION]->CloudAction intent, which
// the shared-projection worker projects after canonical-nodes are committed.
package comprehensive

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// UploadObject uploads content to an S3 bucket using the AWS SDK v2.
// This function intentionally exercises the INVOKES_CLOUD_ACTION edge path so
// the golden-corpus gate can assert that at least one
// Function-[:INVOKES_CLOUD_ACTION]->CloudAction edge materializes (rc-10).
func UploadObject(ctx context.Context, cfg interface{}, bucket, key string, body []byte) error {
	client := s3.NewFromConfig(cfg)
	_, err := client.PutObject(ctx, nil)
	return err
}
