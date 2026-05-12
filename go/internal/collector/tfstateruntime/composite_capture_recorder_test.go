package tfstateruntime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestCompositeCaptureRecorderLogsSkipReason(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	recorder := compositeCaptureLoggingRecorder{
		logger: slog.New(slog.NewJSONHandler(&logBuffer, nil)),
	}

	recorder.Record(context.Background(), terraformstate.CompositeCaptureSkip{
		ResourceType: "aws_iam_user",
		AttributeKey: "secret_block",
		Path:         "resources.*.attributes.secret_block",
		Reason:       terraformstate.CompositeCaptureSkipReasonSensitiveSource,
	})

	logLine := logBuffer.String()
	if !strings.Contains(logLine, `"reason":"known_sensitive_key"`) {
		t.Fatalf("log line = %s, want known_sensitive_key reason", logLine)
	}
	if strings.Contains(logLine, "provider schema does not cover") {
		t.Fatalf("log line = %s, must not claim provider schema is missing for sensitive-source skip", logLine)
	}
}
