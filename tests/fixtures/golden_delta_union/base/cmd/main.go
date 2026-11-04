// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command golden-delta-reader is an IP-free fixture for the golden-corpus
// delta-reader assertions (#10022). main calls one same-file helper and two
// helpers defined in files the delta never touches.
package main

func main() {
	deltaReaderSameFileHelper()
	deltaReaderLedgerTotal()
	deltaReaderReportLine()
}

func deltaReaderSameFileHelper() {}
