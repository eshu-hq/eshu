// Package awssdk adapts AWS SDK for Go v2 SSM Parameter Store responses into
// the scanner-owned metadata model.
//
// The adapter pages DescribeParameters and reads tags with ListTagsForResource,
// recording bounded AWS API telemetry for each call. It deliberately avoids
// value, history, decryption, and mutation APIs so the service package never
// receives parameter values.
package awssdk
