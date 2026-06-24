// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (s ClaimedService) recordGenerationDeadLetter(
	ctx context.Context,
	collected CollectedGeneration,
	failureClass string,
	cause error,
) error {
	if s.DeadLetters == nil {
		return cause
	}

	record := GenerationDeadLetter{
		Scope:            collected.Scope,
		Generation:       collected.Generation,
		FailureClass:     failureClass,
		FailureMessage:   cause.Error(),
		PayloadReference: generationDeadLetterPayloadReference(collected.Scope, collected.Generation),
		DeadLetteredAt:   time.Now().UTC(),
	}
	if err := s.DeadLetters.RecordGenerationDeadLetter(ctx, record); err != nil {
		return errors.Join(cause, fmt.Errorf("record generation dead-letter: %w", err))
	}
	return cause
}

func (s ClaimedService) completeGenerationDeadLetterReplay(
	ctx context.Context,
	collected CollectedGeneration,
) error {
	completer, ok := s.DeadLetters.(GenerationDeadLetterReplayCompleter)
	if !ok {
		return nil
	}
	completionCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		generationDeadLetterReplayCompletionTimeout,
	)
	defer cancel()
	err := completer.CompleteGenerationDeadLetterReplay(completionCtx, GenerationDeadLetterReplayCompletion{
		Scope:       collected.Scope,
		Generation:  collected.Generation,
		CompletedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("complete generation replay: %w", err)
	}
	return nil
}
