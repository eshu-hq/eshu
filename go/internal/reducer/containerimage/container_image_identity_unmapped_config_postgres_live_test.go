// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage_test

import (
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPostgresContainerImageIdentityHandlerKeepsUnmappedConfigWarningRepository(t *testing.T) {
	containerimage.RunUnmappedConfigWarningRepositoryProof(t, func(db *sql.DB) containerimage.ContainerImageIdentityActivationEpochLookup {
		return postgres.NewContainerImageIdentityScopeStateStore(postgres.SQLDB{DB: db})
	})
}
