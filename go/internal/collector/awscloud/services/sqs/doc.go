// Package sqs maps Amazon SQS queue metadata into AWS cloud collector facts.
//
// The package owns scanner-level queue normalization only. It never calls the
// AWS SDK directly, never reads messages, and never persists queue policy JSON.
// SDK adapters provide Queue values, and Scanner emits aws_resource facts plus
// optional dead-letter queue relationship evidence.
package sqs
