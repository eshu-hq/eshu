// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deltareader is an IP-free fixture for the golden-corpus
// delta-reader assertions (#10022). deltaReaderRun calls one same-file helper
// and two helpers defined in files the delta never touches.
package deltareader

func deltaReaderRun() {
	deltaReaderSameFileHelper()
	deltaReaderLedgerTotal()
	deltaReaderReportLine()
}

func deltaReaderSameFileHelper() {}
