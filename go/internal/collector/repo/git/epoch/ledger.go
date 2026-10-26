// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package epoch

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

// RepositoryBucket is the catch-all bucket for facts that name no single
// fixture file: the repository fact, follow-up envelopes, the documentation
// source, and any fact whose path is not a fixture file. Its input digest
// covers the whole fixture tree.
const RepositoryBucket = "_repository"

// LedgerFile is the committed ledger, relative to this package directory. The
// git package's gate test rewrites it; production code reads the embedded copy.
const LedgerFile = "ledger.tsv"

// ledgerHeader is written above the epoch line. Comment lines never change, so
// they never conflict.
const ledgerHeader = `# Git collector content epoch ledger (#9097). Generated; do not edit by hand.
# Re-pin: cd go && go test ./internal/collector/repo/git -run TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin
# On a merge conflict: git checkout origin/main -- go/internal/collector/repo/git/epoch/ledger.tsv, then re-pin.
# epoch	<epoch>	<seal>
`

// bucketHeader sits between the epoch line and the first bucket line. Git
// merges two changes as one conflict when they touch, so this fixed line keeps
// a change to the first bucket out of the epoch line's conflict hunk.
const bucketHeader = "# bucket\tinput_digest\tfact_count\toutput_digest\n"

// Bucket is one ledger line: the facts the git collector emits for one
// fixture file (or the RepositoryBucket catch-all) under the current code.
type Bucket struct {
	// Name is the fixture-relative, slash-separated file path, or
	// RepositoryBucket.
	Name string
	// InputDigest is the sha256 of the fixture file's bytes; for
	// RepositoryBucket, of the whole fixture tree.
	InputDigest string
	// FactCount is the number of facts attributed to the bucket.
	FactCount int
	// OutputDigest is the sha256 of the bucket's sorted, normalized fact lines.
	OutputDigest string
}

// Ledger is the parsed content epoch ledger. Epoch is the value folded into
// every git freshness hint; Seal binds the epoch line to the bucket lines the
// same re-pin wrote, so a hand merge that combines bucket lines from two
// branches under one side's epoch line is detectable (it is not Sealed).
type Ledger struct {
	Epoch   string
	Seal    string
	Buckets []Bucket
}

// Parse reads a ledger. It rejects a missing or repeated epoch line, an empty
// epoch, a bucket line without exactly four fields, a non-numeric or negative
// fact count, and bucket lines that are duplicated or not in byte order.
// Lines starting with "#" and blank lines are ignored. Parse does not check the
// seal or the epoch format; use Sealed and ValidEpoch.
func Parse(data []byte) (Ledger, error) {
	var ledger Ledger
	sawEpoch := false
	for number, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if fields[0] == "epoch" {
			if sawEpoch {
				return Ledger{}, fmt.Errorf("line %d: second epoch line", number+1)
			}
			if len(fields) != 3 || fields[1] == "" || fields[2] == "" {
				return Ledger{}, fmt.Errorf("line %d: epoch line wants \"epoch<TAB>value<TAB>seal\"", number+1)
			}
			ledger.Epoch, ledger.Seal, sawEpoch = fields[1], fields[2], true
			continue
		}
		if len(fields) != 4 {
			return Ledger{}, fmt.Errorf("line %d: bucket line has %d fields, want 4", number+1, len(fields))
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil || count < 0 {
			return Ledger{}, fmt.Errorf("line %d: fact count %q is not a non-negative integer", number+1, fields[2])
		}
		if n := len(ledger.Buckets); n > 0 && ledger.Buckets[n-1].Name >= fields[0] {
			return Ledger{}, fmt.Errorf("line %d: bucket %q is duplicated or out of order", number+1, fields[0])
		}
		ledger.Buckets = append(ledger.Buckets, Bucket{
			Name: fields[0], InputDigest: fields[1], FactCount: count, OutputDigest: fields[3],
		})
	}
	if !sawEpoch {
		return Ledger{}, fmt.Errorf("ledger has no epoch line")
	}
	return ledger, nil
}

// Marshal renders the ledger: the fixed comment header, the epoch line, and
// one bucket line per bucket in the order held (Repin keeps them sorted).
func (l Ledger) Marshal() []byte {
	var out bytes.Buffer
	out.WriteString(ledgerHeader)
	fmt.Fprintf(&out, "epoch\t%s\t%s\n", l.Epoch, l.Seal)
	out.WriteString(bucketHeader)
	out.Write(bucketLines(l.Buckets))
	return out.Bytes()
}

// Sealed reports whether the seal on the epoch line matches the bucket lines.
func (l Ledger) Sealed() bool {
	return l.Seal == SealOf(l.Buckets)
}

// SealOf returns the sha256 of the bucket lines as Marshal writes them.
func SealOf(buckets []Bucket) string {
	return fmt.Sprintf("%x", sha256.Sum256(bucketLines(buckets)))
}

func bucketLines(buckets []Bucket) []byte {
	var out bytes.Buffer
	for _, b := range buckets {
		fmt.Fprintf(&out, "%s\t%s\t%d\t%s\n", b.Name, b.InputDigest, b.FactCount, b.OutputDigest)
	}
	return out.Bytes()
}
