// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

type GenerationDeadLetterJSON struct {
	DeadLetter              int     `json:"dead_letter"`
	ReplayRequested         int     `json:"replay_requested"`
	ReplayAttempts          int     `json:"replay_attempts"`
	OldestDeadLetterAge     string  `json:"oldest_dead_letter_age"`
	OldestDeadLetterSeconds float64 `json:"oldest_dead_letter_age_seconds"`
}

func GenerationDeadLetterJSONFrom(
	snapshot GenerationDeadLetterSnapshot,
) GenerationDeadLetterJSON {
	snapshot = CloneGenerationDeadLetterSnapshot(snapshot)
	return GenerationDeadLetterJSON{
		DeadLetter:              snapshot.DeadLetter,
		ReplayRequested:         snapshot.ReplayRequested,
		ReplayAttempts:          snapshot.ReplayAttempts,
		OldestDeadLetterAge:     snapshot.OldestDeadLetterAge.String(),
		OldestDeadLetterSeconds: snapshot.OldestDeadLetterAge.Seconds(),
	}
}
