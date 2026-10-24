// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func bindAwaitTarget(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, runID int64, attempt int) context.Context {
	if runID <= 0 || attempt <= 0 {
		return ctx
	}
	input, err := readTestedInput(ctx, runner, repo, pr, headSHA, runID, attempt)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "required-gates: target caller input unbound (%v); transfers disabled\n", err)
		return ctx
	}
	return withTestedInput(ctx, input)
}

func tryProvenReuse(ctx context.Context, runner ghRunner, reg *cigates.Registry, root, repo string, pr int, headSHA string, pollInterval time.Duration, proofFile string) (bool, error) {
	input, bound := currentTestedInput(ctx)
	if !bound || input.RunStatus != "completed" || input.RunConclusion != "success" {
		return false, nil
	}
	verdict, err := probeReuseForAwait(ctx, runner, reg, repo, pr, headSHA)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "required-gates: reuse probe failed (%v); awaiting gates normally\n", err)
		return false, nil
	}
	if !verdict.Reuse {
		return false, nil
	}
	transferred, transferErr := transferProvenReuse(ctx, runner, root, repo, pr, headSHA, pollInterval, verdict)
	if !transferred {
		return transferErr != nil, transferErr
	}
	if err := writeTransferProof(proofFile, transferProof{Mode: "reuse", Input: input, FullRootSHA: verdict.FullRootSHA}); err != nil {
		return true, err
	}
	return true, transferErr
}

func writeFullProofIfCovered(ctx context.Context, runner ghRunner, repo, root string, paths []string, truncated bool, selected []resolvedRequiredGate, proofFile string) error {
	input, bound := currentTestedInput(ctx)
	if !bound || input.RunStatus != "completed" || input.RunConclusion != "success" {
		return nil
	}
	covered, err := proveFullCoverage(ctx, runner, repo, input, selected, root, paths, truncated)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "required-gates: full input coverage unbound (%v); ordinary success remains unmarked\n", err)
		return nil
	}
	if !covered {
		return nil
	}
	return writeTransferProof(proofFile, transferProof{Mode: "full", Input: input})
}

func writeSmokeProof(ctx context.Context, proofFile string, smoke smokeEvidence) error {
	input, _ := currentTestedInput(ctx)
	return writeTransferProof(proofFile, transferProof{Mode: "smoke", Input: input, FullRootSHA: smoke.FullRootSHA})
}

func smokeModeProven(ctx context.Context, runner ghRunner, repo string) (bool, error) {
	input, bound := currentTestedInput(ctx)
	if !bound || input.RunStatus != "completed" || input.RunConclusion != "success" {
		return false, nil
	}
	return planModeProven(ctx, runner, repo, input, "smoke")
}
