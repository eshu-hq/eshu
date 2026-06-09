package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// serviceMaterializationWriterFor builds the additive per-service ownership
// generation lineage writer (#1943) over the shared reducer database. When the
// database does not expose a transaction beginner the writer is nil, so the
// service-catalog correlation handler keeps its existing behavior unchanged.
func serviceMaterializationWriterFor(database postgres.ExecQueryer) reducer.ServiceMaterializationWriter {
	beginner := reducerBeginner(database)
	if beginner == nil {
		return nil
	}
	return reducer.PostgresServiceMaterializationWriter{
		DB: postgres.ServiceMaterializationBeginner{Beginner: beginner},
	}
}
