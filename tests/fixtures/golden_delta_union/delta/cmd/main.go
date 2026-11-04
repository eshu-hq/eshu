// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command golden-delta-reader is an IP-free fixture for the golden-corpus
// delta-reader assertions (#10022). main calls one same-file helper and two
// helpers defined in files the delta never touches.
package main

// main is the only function the delta generation re-parses (#10022).
func main() {
	deltaReaderSameFileHelper()
	deltaReaderLedgerTotal()
	deltaReaderReportLine()
}

func deltaReaderSameFileHelper() {}
