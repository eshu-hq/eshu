// Package main wires the Kubernetes live collector binary.
//
// The binary connects to one or more configured Kubernetes clusters with
// read-only credentials (a kubeconfig file or the in-cluster service account),
// lists a fixed core resource set (namespaces, pods, deployments, replicasets,
// services, ingresses), maps those objects into typed kubernetes_live facts
// (pod_template, relationship, warning), and commits them through the shared
// Postgres ingestion store via collector.Service.
//
// The collector is read-only and metadata-only by construction: it never
// mutates the cluster and never reads Secret values, ConfigMap data payloads,
// environment variable values, or container logs. It is the foundation toward
// issue #388; claim-driven collection, watch mode, and additional resource
// kinds are follow-up work.
package main
