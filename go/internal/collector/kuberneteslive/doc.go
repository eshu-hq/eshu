// Package kuberneteslive implements the read-only Kubernetes live collector
// source and its fact-envelope contract.
//
// The collector observes a configured cluster's API server with read-only
// credentials, lists a fixed core resource set (namespaces, pods, deployments,
// replicasets, services, ingresses), and maps those objects into three typed
// source facts: kubernetes_live.pod_template, kubernetes_live.relationship, and
// kubernetes_live.warning. Facts are emitted through the shared collector
// envelope and committed by collector.Service; this package never writes graph
// state and never resolves canonical ownership or drift, which remain reducer
// responsibilities (issue #388).
//
// The package is backend-neutral: the Kubernetes API surface is the narrow,
// read-only Client interface, so the source is unit-testable with fakes and
// carries no client-go import. The client-go adapter lives in the clientgo
// subpackage.
//
// Redaction is a construction invariant. The collector is metadata-only: it
// emits image references, environment variable NAMES, declared ports, service
// account, selector, and label metadata. It never emits Secret values,
// ConfigMap data payloads, environment variable values, or container logs.
package kuberneteslive
