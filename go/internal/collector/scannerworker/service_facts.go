// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import "github.com/eshu-hq/eshu/go/internal/facts"

const scannerFactChannelBuffer = 16

func factChannel(values []facts.Envelope) <-chan facts.Envelope {
	ch := make(chan facts.Envelope, scannerFactChannelBuffer)
	go func() {
		defer close(ch)
		for _, value := range values {
			ch <- value
		}
	}()
	return ch
}
