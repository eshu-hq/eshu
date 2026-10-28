// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main wires the image-unpacking scanner component binary.
//
// The binary reads one collector-sdk/v1alpha3 host request from stdin and
// writes exactly one result page document to stdout, executing the in-tree
// image analyzer behind the bounded stdio contract. It owns no claim, no
// store, and no network: the host validated the claim, the activation, and
// the producer grants before launch, and the binary emits source facts only.
// Structured log lines go to stderr; stdout carries the result document
// alone. Refs #8878.
package main
