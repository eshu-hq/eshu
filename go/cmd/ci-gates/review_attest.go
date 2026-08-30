// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const reviewAttestationSchema = "eshu.review-attestation.v1"

type reviewAttestation struct {
	SchemaVersion     string `json:"schema_version"`
	CapturedAt        string `json:"captured_at"`
	RepositorySHA256  string `json:"repository_sha256"`
	BaseRef           string `json:"base_ref"`
	BaseCommit        string `json:"base_commit"`
	BaseTree          string `json:"base_tree"`
	MergeBase         string `json:"merge_base"`
	HeadRef           string `json:"head_ref"`
	HeadCommit        string `json:"head_commit"`
	HeadTree          string `json:"head_tree"`
	BinaryDiffSHA256  string `json:"binary_diff_sha256"`
	RawDiffSHA256     string `json:"raw_diff_sha256"`
	CommitRangeSHA256 string `json:"commit_range_sha256"`
	StatusSHA256      string `json:"status_sha256"`
	SubmodulesSHA256  string `json:"submodules_sha256"`
	ClaimsSHA256      string `json:"claims_sha256"`
	ReviewPacketSHA   string `json:"review_packet_sha256"`
	VerdictSHA256     string `json:"verdict_sha256"`
	WorktreeClean     bool   `json:"worktree_clean"`
}

type reviewAttestOptions struct {
	repoRoot    string
	base        string
	claimsFile  string
	packetFile  string
	verdictFile string
	receiptFile string
}

func runReviewAttest(args []string) error {
	if len(args) == 0 || (args[0] != "capture" && args[0] != "verify") {
		return fmt.Errorf("expected capture or verify")
	}
	action := args[0]
	fs := flag.NewFlagSet("review-attest "+action, flag.ContinueOnError)
	repoRoot := fs.String("repo-root", "", "repository root (default: git toplevel)")
	base := fs.String("base", "origin/main", "review base ref")
	claims := fs.String("claims-file", "", "file containing the exact PR title and body bytes")
	packet := fs.String("review-packet", "", "file containing the reviewed diff packet")
	verdict := fs.String("verdict", "", "file containing the full semantic review verdict")
	receipt := fs.String("receipt", "", "local JSON receipt path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	options := reviewAttestOptions{
		repoRoot:    root,
		base:        *base,
		claimsFile:  *claims,
		packetFile:  *packet,
		verdictFile: *verdict,
		receiptFile: *receipt,
	}
	if err := options.validate(); err != nil {
		return err
	}
	current, err := collectReviewAttestation(options)
	if err != nil {
		return err
	}
	if !current.WorktreeClean {
		return fmt.Errorf("working tree is not clean; a semantic review cannot bind mutable bytes")
	}
	if action == "capture" {
		if err := writeReviewAttestation(options.receiptFile, current); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "PASS: captured review attestation for %s\n", current.HeadCommit[:12])
		return nil
	}
	stored, err := readReviewAttestation(options.receiptFile)
	if err != nil {
		return err
	}
	if field, before, after := firstReviewAttestationChange(stored, current); field != "" {
		return fmt.Errorf("%s changed (%s -> %s); repeat the full semantic review", field, shortHash(before), shortHash(after))
	}
	_, _ = fmt.Fprintln(os.Stdout, "PASS: review attestation matches exact inputs")
	return nil
}

func (o reviewAttestOptions) validate() error {
	for name, value := range map[string]string{
		"--base":          o.base,
		"--claims-file":   o.claimsFile,
		"--review-packet": o.packetFile,
		"--verdict":       o.verdictFile,
		"--receipt":       o.receiptFile,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func collectReviewAttestation(options reviewAttestOptions) (reviewAttestation, error) {
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", append([]string{"-C", options.repoRoot}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return out, nil
	}
	text := func(args ...string) (string, error) {
		out, err := git(args...)
		return strings.TrimSpace(string(out)), err
	}
	headRef, err := text("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || headRef == "" {
		return reviewAttestation{}, fmt.Errorf("HEAD must be a named branch")
	}
	if headRef == "main" || headRef == "master" {
		return reviewAttestation{}, fmt.Errorf("review attestation is not allowed on %s", headRef)
	}
	baseCommit, err := text("rev-parse", "--verify", options.base+"^{commit}")
	if err != nil {
		return reviewAttestation{}, err
	}
	baseTree, err := text("show", "-s", "--format=%T", baseCommit)
	if err != nil {
		return reviewAttestation{}, err
	}
	headCommit, err := text("rev-parse", "HEAD^{commit}")
	if err != nil {
		return reviewAttestation{}, err
	}
	headTree, err := text("show", "-s", "--format=%T", headCommit)
	if err != nil {
		return reviewAttestation{}, err
	}
	mergeBase, err := text("merge-base", baseCommit, headCommit)
	if err != nil {
		return reviewAttestation{}, err
	}
	commonDir, err := text("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return reviewAttestation{}, err
	}
	binaryDiff, err := git("diff", "--binary", "--full-index", "--no-ext-diff", mergeBase, headCommit)
	if err != nil {
		return reviewAttestation{}, err
	}
	rawDiff, err := git("diff", "--raw", "--no-abbrev", "-z", "--no-renames", mergeBase, headCommit)
	if err != nil {
		return reviewAttestation{}, err
	}
	commitRange, err := git("log", "--format=%H%x00%P%x00%T%x00%B%x00", "-z", mergeBase+".."+headCommit)
	if err != nil {
		return reviewAttestation{}, err
	}
	status, err := git("status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return reviewAttestation{}, err
	}
	submodules, err := git("submodule", "status", "--recursive")
	if err != nil {
		return reviewAttestation{}, err
	}
	claimsHash, err := fileSHA256(options.claimsFile)
	if err != nil {
		return reviewAttestation{}, fmt.Errorf("hash claims file: %w", err)
	}
	packetHash, err := fileSHA256(options.packetFile)
	if err != nil {
		return reviewAttestation{}, fmt.Errorf("hash review packet: %w", err)
	}
	verdictHash, err := fileSHA256(options.verdictFile)
	if err != nil {
		return reviewAttestation{}, fmt.Errorf("hash verdict: %w", err)
	}
	return reviewAttestation{
		SchemaVersion:     reviewAttestationSchema,
		CapturedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		RepositorySHA256:  bytesSHA256([]byte(commonDir)),
		BaseRef:           options.base,
		BaseCommit:        baseCommit,
		BaseTree:          baseTree,
		MergeBase:         mergeBase,
		HeadRef:           headRef,
		HeadCommit:        headCommit,
		HeadTree:          headTree,
		BinaryDiffSHA256:  bytesSHA256(binaryDiff),
		RawDiffSHA256:     bytesSHA256(rawDiff),
		CommitRangeSHA256: bytesSHA256(commitRange),
		StatusSHA256:      bytesSHA256(status),
		SubmodulesSHA256:  bytesSHA256(submodules),
		ClaimsSHA256:      claimsHash,
		ReviewPacketSHA:   packetHash,
		VerdictSHA256:     verdictHash,
		WorktreeClean:     len(status) == 0,
	}, nil
}

func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return bytesSHA256(raw), nil
}

func bytesSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func writeReviewAttestation(path string, receipt reviewAttestation) error {
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode review receipt: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create review receipt directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".review-attestation-*")
	if err != nil {
		return fmt.Errorf("create review receipt: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish review receipt: %w", err)
	}
	return nil
}

func readReviewAttestation(path string) (reviewAttestation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return reviewAttestation{}, fmt.Errorf("read review receipt: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var receipt reviewAttestation
	if err := decoder.Decode(&receipt); err != nil {
		return reviewAttestation{}, fmt.Errorf("decode review receipt: %w", err)
	}
	if receipt.SchemaVersion != reviewAttestationSchema {
		return reviewAttestation{}, fmt.Errorf("unsupported review receipt schema %q", receipt.SchemaVersion)
	}
	return receipt, nil
}

func firstReviewAttestationChange(before, after reviewAttestation) (string, string, string) {
	fields := []struct {
		name   string
		before string
		after  string
	}{
		{"repository_sha256", before.RepositorySHA256, after.RepositorySHA256},
		{"base_ref", before.BaseRef, after.BaseRef},
		{"base_commit", before.BaseCommit, after.BaseCommit},
		{"base_tree", before.BaseTree, after.BaseTree},
		{"merge_base", before.MergeBase, after.MergeBase},
		{"head_ref", before.HeadRef, after.HeadRef},
		{"head_commit", before.HeadCommit, after.HeadCommit},
		{"head_tree", before.HeadTree, after.HeadTree},
		{"binary_diff_sha256", before.BinaryDiffSHA256, after.BinaryDiffSHA256},
		{"raw_diff_sha256", before.RawDiffSHA256, after.RawDiffSHA256},
		{"commit_range_sha256", before.CommitRangeSHA256, after.CommitRangeSHA256},
		{"status_sha256", before.StatusSHA256, after.StatusSHA256},
		{"submodules_sha256", before.SubmodulesSHA256, after.SubmodulesSHA256},
		{"claims_sha256", before.ClaimsSHA256, after.ClaimsSHA256},
		{"review_packet_sha256", before.ReviewPacketSHA, after.ReviewPacketSHA},
		{"verdict_sha256", before.VerdictSHA256, after.VerdictSHA256},
	}
	for _, field := range fields {
		if field.before != field.after {
			return field.name, field.before, field.after
		}
	}
	return "", "", ""
}

func shortHash(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
