package status

import (
	"fmt"
	"strconv"
	"strings"
)

const queueFailureTextLimit = 240

func cloneQueueFailure(snapshot *QueueFailureSnapshot) *QueueFailureSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}

func queueFailureText(snapshot *QueueFailureSnapshot) string {
	if snapshot == nil {
		return ""
	}

	parts := []string{
		fmt.Sprintf("stage=%s", snapshot.Stage),
		fmt.Sprintf("domain=%s", snapshot.Domain),
		fmt.Sprintf("status=%s", snapshot.Status),
		fmt.Sprintf("class=%s", snapshot.FailureClass),
	}
	if message := boundedQueueFailureText(snapshot.FailureMessage); message != "" {
		parts = append(parts, fmt.Sprintf("message=%s", strconv.Quote(message)))
	}
	if details := boundedQueueFailureText(snapshot.FailureDetails); details != "" {
		parts = append(parts, fmt.Sprintf("details=%s", strconv.Quote(details)))
	}

	return strings.Join(parts, " ")
}

func boundedQueueFailureText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= queueFailureTextLimit {
		return value
	}
	return value[:queueFailureTextLimit] + "..."
}
