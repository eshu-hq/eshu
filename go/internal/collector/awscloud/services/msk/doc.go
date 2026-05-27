// Package msk maps Amazon Managed Streaming for Apache Kafka metadata into AWS
// cloud collector facts.
//
// The scanner emits reported-confidence cluster, broker configuration, and
// replicator resources plus relationships for subnet, security group, KMS key,
// IAM role, and configuration dependencies. Mutation APIs, raw broker
// server.properties bodies, broker log contents, bootstrap broker endpoints,
// SCRAM secret material, Kafka topic data, and Kafka message contents stay
// outside this package contract.
package msk
