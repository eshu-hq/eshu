// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package resiliencehub maps AWS Resilience Hub application, resiliency policy,
// application component, input source, and assessment metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for applications, resiliency
// policies, application components, input sources, and assessments, plus
// relationships for app-uses-policy, app-protects-resource (only for physical
// resources Resilience Hub identifies by an ARN that the owning scanner also
// keys by ARN), component-in-app, input-source-in-app, and assessment-for-app
// evidence. Assessment result bodies, drift detail, recommendation contents, and
// any data-plane payload stay outside this package contract: the scanner is
// metadata-only.
package resiliencehub
