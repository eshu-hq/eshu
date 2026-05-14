// Package awssdk adapts AWS SDK for Go v2 API Gateway control-plane responses
// into scanner-owned metadata records.
//
// The adapter pages REST API Gateway and API Gateway v2 read-only operations,
// maps SDK response shapes into the parent package's safe metadata model, and
// records shared AWS collector API-call telemetry around every AWS operation.
// It does not call API execution, export, API key, authorizer secret, or
// mutation APIs. Callers must handle AWS authorization, throttling, and partial
// service failures as normal scanner errors.
package awssdk
