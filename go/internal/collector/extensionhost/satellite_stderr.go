// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"encoding/json"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

const (
	// maxStderrRecordBytes bounds one stderr line the relay will parse. A
	// longer line is skipped, so a satellite cannot make the host decode an
	// arbitrarily large JSON value.
	maxStderrRecordBytes = 4 * 1024
	// maxScannedStderrBytes bounds how much of the stderr buffer the relay
	// scans, whatever stderr_limit_bytes the operator configured. It equals
	// the default stderr buffer, so the default path scans all of it.
	maxScannedStderrBytes = 64 * 1024
)

// InputLimitFigures are the byte figures a satellite prints on its stderr
// record when a claim fails as input_limit_exceeded (eshu-hq/eshu#9823). They
// are numbers only: the host relays an allowlist of three int64 keys and never
// a satellite string, a raw line, or any other key.
type InputLimitFigures struct {
	// InflatedBytes is the decompressed bytes counted when the budget broke.
	InflatedBytes int64
	// BudgetBytes is the decompressed-byte budget that applied.
	BudgetBytes int64
	// CompressedBytes is the sum of the layer file sizes read so far.
	CompressedBytes int64
}

// stderrFiguresRecord is the allowlist. encoding/json drops every key not
// named here, and a value that is not a JSON integer fitting int64 fails the
// whole line, so the host holds no string from the satellite.
type stderrFiguresRecord struct {
	InflatedBytes   *int64 `json:"inflated_bytes"`
	BudgetBytes     *int64 `json:"budget_bytes"`
	CompressedBytes *int64 `json:"compressed_bytes"`
}

// claimRelay carries satellite figures from the paged runner loop back to the
// one ResultRecord NextClaimed reports. It is per call, so concurrent claims
// never share it.
type claimRelay struct {
	inputLimit *InputLimitFigures
}

var inflatedBytesKey = []byte(`"inflated_bytes"`)

// parseInputLimitFigures extracts the allowlisted figures from satellite
// stderr: the last line, within the first maxScannedStderrBytes, that is one
// JSON object carrying all three figures as non-negative integers. It returns
// nil when there is none. Lines over maxStderrRecordBytes, non-JSON lines, and
// records with a missing, null, negative, fractional, overflowing, or string
// figure are skipped whole, never partially relayed.
func parseInputLimitFigures(stderr []byte) *InputLimitFigures {
	if len(stderr) > maxScannedStderrBytes {
		stderr = stderr[:maxScannedStderrBytes]
	}
	var found *InputLimitFigures
	for len(stderr) > 0 {
		line := stderr
		if end := bytes.IndexByte(stderr, '\n'); end >= 0 {
			line, stderr = stderr[:end], stderr[end+1:]
		} else {
			stderr = nil
		}
		if len(line) > maxStderrRecordBytes || !bytes.Contains(line, inflatedBytesKey) {
			continue
		}
		var record stderrFiguresRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.InflatedBytes == nil || record.BudgetBytes == nil || record.CompressedBytes == nil {
			continue
		}
		if *record.InflatedBytes < 0 || *record.BudgetBytes < 0 || *record.CompressedBytes < 0 {
			continue
		}
		found = &InputLimitFigures{
			InflatedBytes:   *record.InflatedBytes,
			BudgetBytes:     *record.BudgetBytes,
			CompressedBytes: *record.CompressedBytes,
		}
	}
	return found
}

// attachStderrFigures sets call.InputLimit from the satellite's stderr when
// the call's result is neither complete nor unchanged. A complete call never
// costs a parse and never relays, so the success path stays unchanged.
func attachStderrFigures(call CallResult, stderr []byte) CallResult {
	if state := call.Result.State; state == sdkcollector.ResultComplete || state == sdkcollector.ResultUnchanged {
		return call
	}
	call.InputLimit = parseInputLimitFigures(stderr)
	return call
}

// decodeCallWithStderr decodes one paged satellite call and attaches the
// allowlisted stderr figures. Both runners share it so they relay identically.
func decodeCallWithStderr(stdout, stderr []byte) (CallResult, error) {
	call, err := decodePagedSDKResult(stdout)
	if err != nil {
		return CallResult{}, err
	}
	return attachStderrFigures(call, stderr), nil
}
