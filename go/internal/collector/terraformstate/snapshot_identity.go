package terraformstate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SnapshotIdentity is the top-level Terraform state identity needed to build a
// durable state-snapshot generation before full fact parsing.
type SnapshotIdentity struct {
	Serial  int64
	Lineage string
}

// ReadSnapshotIdentity streams the top-level serial and lineage fields from a
// Terraform state reader without decoding or retaining resource/output values.
func ReadSnapshotIdentity(ctx context.Context, reader io.Reader) (SnapshotIdentity, error) {
	if err := ctx.Err(); err != nil {
		return SnapshotIdentity{}, err
	}
	if reader == nil {
		return SnapshotIdentity{}, fmt.Errorf("terraform state reader must not be nil")
	}

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return SnapshotIdentity{}, fmt.Errorf("read terraform state object: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return SnapshotIdentity{}, fmt.Errorf("terraform state root must be an object")
	}

	var identity SnapshotIdentity
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return SnapshotIdentity{}, fmt.Errorf("read terraform state key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return SnapshotIdentity{}, fmt.Errorf("terraform state object key must be a string")
		}
		switch key {
		case "serial":
			if err := decoder.Decode(&identity.Serial); err != nil {
				return SnapshotIdentity{}, fmt.Errorf("decode terraform state serial: %w", err)
			}
		case "lineage":
			if err := decoder.Decode(&identity.Lineage); err != nil {
				return SnapshotIdentity{}, fmt.Errorf("decode terraform state lineage: %w", err)
			}
		default:
			if err := skipValue(decoder); err != nil {
				return SnapshotIdentity{}, err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return SnapshotIdentity{}, fmt.Errorf("close terraform state object: %w", err)
	}
	if strings.TrimSpace(identity.Lineage) == "" {
		return SnapshotIdentity{}, fmt.Errorf("terraform state lineage must not be blank")
	}
	if identity.Serial < 0 {
		return SnapshotIdentity{}, fmt.Errorf("terraform state serial must not be negative")
	}
	return identity, nil
}
