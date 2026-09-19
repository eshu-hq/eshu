// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// uploads.go exercises the value-flow cloud-sink path (issue #6785 fixture):
// a proven net/http route handler that takes an *http.Request source and calls
// the AWS SDK v2 S3 client's PutObject, mirroring
// tests/fixtures/ecosystems/go_comprehensive/cloud_actions.go's receiver-binding
// shape (rc-10) and
// tests/fixtures/ecosystems/go_comprehensive/routes/handlers.go's net/http mux
// shape (rc-8/rc-2). The handler resolves through this repo's existing
// workload:deployable-source / workload-instance:deployable-source:prod
// binding, so a matching CAN_PERFORM edge from the awscloud cassette's
// deployable-source-app-role principal lets the reducer's value-flow fixpoint
// find a same-function TAINT_FLOWS_TO self-loop from the *http.Request param to
// the s3:putobject sink.
package main

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// registerUploadRoutes binds StoreUploadReceipt onto the default net/http mux so
// the parser's route-entry detection resolves it as a handles_route entry
// (rc-8 shape) and the reducer's RUNS_IN materialization binds it to this
// repo's Workload.
func registerUploadRoutes() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /uploads", StoreUploadReceipt)
}

// StoreUploadReceipt receives an upload request and writes the receipt body to
// S3. The *http.Request parameter is a proven taint source
// (goSourceParamTypeMarkers: "http.Request"); the s3 client created by the
// SDK v2 constructor s3.New (a recognized awsSDKConstructorNames entry, called
// with a type-correct zero s3.Options) is a proven INVOKES_CLOUD_ACTION
// receiver binding, so the interproc value-flow fixpoint can trace the request
// source to the s3:putobject sink within this one function.
func StoreUploadReceipt(w http.ResponseWriter, r *http.Request) {
	client := s3.New(s3.Options{})
	_, _ = client.PutObject(context.Background(), nil)
}
