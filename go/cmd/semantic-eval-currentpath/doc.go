// Package main runs the semantic eval current-path baseline helper.
//
// The binary reads a currentpath eval suite, executes each case against an Eshu
// API URL, scores the observed run with semanticeval, and writes machine-readable
// run/report JSON. It is a one-shot maintainer tool for Phase 0 of the
// NornicDB semantic retrieval evaluation ADR, not a long-running runtime.
package main
