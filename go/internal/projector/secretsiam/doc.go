// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package secretsiam builds the secrets/IAM trust-chain reducer intent from
// one immutable scope generation: when the generation carries at least one
// fact whose kind the central secrets/IAM posture schema registry recognizes
// (AWS IAM principals and policies, GCP IAM, Kubernetes service accounts and
// RBAC, EKS identity associations, Vault mounts and policies, coverage
// warnings), it asks the reducer to expand the trust chain for that scope
// generation once. The anchor is the earliest recognized posture fact in
// original input order across every posture kind — there is no per-kind
// priority — so the reducer claim is stable across reprojections of the same
// generation. Only envelope metadata is read; no payload is decoded, and
// schema-version admission stays with root projection, which rejects an
// unsupported secrets/IAM schema version before any builder runs. The
// source-system label falls back from the fact's SourceRef to its
// CollectorKind to the literal "secrets_iam_posture", a third tier the shared
// intent helper does not have, which is why this builder keeps its own
// helper. Posture facts stay provenance-only here: the reducer's
// secrets_iam_trust_chain handler owns every join and never receives a
// derived access path from the projector. Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package secretsiam
