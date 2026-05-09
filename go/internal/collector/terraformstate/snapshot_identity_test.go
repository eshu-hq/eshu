package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestReadSnapshotIdentityStreamsTopLevelSerialAndLineage(t *testing.T) {
	t.Parallel()

	state := `{
		"format_version": "1.0",
		"outputs": {"password": {"sensitive": true, "value": "not-returned"}},
		"serial": 17,
		"resources": [{"mode":"managed","type":"aws_s3_bucket","name":"logs","instances":[]}],
		"lineage": "lineage-123"
	}`

	identity, err := terraformstate.ReadSnapshotIdentity(context.Background(), strings.NewReader(state))
	if err != nil {
		t.Fatalf("ReadSnapshotIdentity() error = %v, want nil", err)
	}
	if got, want := identity.Serial, int64(17); got != want {
		t.Fatalf("Serial = %d, want %d", got, want)
	}
	if got, want := identity.Lineage, "lineage-123"; got != want {
		t.Fatalf("Lineage = %q, want %q", got, want)
	}
}

func TestReadSnapshotIdentityRejectsMissingLineage(t *testing.T) {
	t.Parallel()

	_, err := terraformstate.ReadSnapshotIdentity(context.Background(), strings.NewReader(`{"serial":17}`))
	if err == nil {
		t.Fatal("ReadSnapshotIdentity() error = nil, want non-nil")
	}
}
