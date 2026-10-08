// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// driftBaseDDL creates one object of each fingerprinted family: a CHECK
// constraint, a foreign key, a trigger backed by a plpgsql routine, a second
// routine the trigger can rebind to, and an expression index with an INCLUDE
// column and a partial predicate.
const driftBaseDDL = `
CREATE TABLE drift_t1(id int PRIMARY KEY, v int CHECK (v > 0));
CREATE TABLE drift_t2(id int REFERENCES drift_t1(id) ON DELETE CASCADE);
CREATE TABLE drift_t3(id int PRIMARY KEY);
CREATE OR REPLACE FUNCTION drift_bump() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.v := NEW.v + 1; RETURN NEW; END; $$;
CREATE OR REPLACE FUNCTION drift_bump2() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.v := NEW.v + 2; RETURN NEW; END; $$;
CREATE TRIGGER drift_t1_bump BEFORE UPDATE ON drift_t1 FOR EACH ROW EXECUTE FUNCTION drift_bump();
CREATE INDEX drift_t1_v_expr ON drift_t1 ((v + 1)) INCLUDE (id) WHERE v > 10;`

// TestReducerFairnessIsolatedSchemaDefinitionDriftLive is the seeded-violation
// proof for #7693: each subtest builds two identical small schemas, mutates
// one object definition on one side without renaming it, and requires the
// isolated-schema inventory comparison to report drift. A same-name definition
// change the fingerprints miss fails its subtest. The index-include and
// index-predicate subtests are preservation locks: indkey and indpred already
// catch those shapes, and the strengthened index arm must keep catching them.
func TestReducerFairnessIsolatedSchemaDefinitionDriftLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the definition-drift guard")
	}

	mutations := []struct {
		name   string
		prefix string
		ddl    string
	}{
		{
			name:   "check-predicate",
			prefix: "con:drift_t1_v_check",
			ddl:    `ALTER TABLE drift_t1 DROP CONSTRAINT drift_t1_v_check; ALTER TABLE drift_t1 ADD CHECK (v > 100);`,
		},
		{
			name:   "fk-retarget",
			prefix: "con:drift_t2_id_fkey",
			ddl:    `ALTER TABLE drift_t2 DROP CONSTRAINT drift_t2_id_fkey; ALTER TABLE drift_t2 ADD FOREIGN KEY (id) REFERENCES drift_t3(id) ON DELETE CASCADE;`,
		},
		{
			name:   "trigger-rebind",
			prefix: "trg:drift_t1_bump",
			ddl:    `DROP TRIGGER drift_t1_bump ON drift_t1; CREATE TRIGGER drift_t1_bump BEFORE UPDATE ON drift_t1 FOR EACH ROW EXECUTE FUNCTION drift_bump2();`,
		},
		{
			name:   "routine-body",
			prefix: "fn:drift_bump",
			ddl:    `CREATE OR REPLACE FUNCTION drift_bump() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.v := NEW.v + 100; RETURN NEW; END; $$;`,
		},
		{
			name:   "index-include",
			prefix: "idx:drift_t1_v_expr",
			ddl:    `DROP INDEX drift_t1_v_expr; CREATE INDEX drift_t1_v_expr ON drift_t1 ((v + 1)) WHERE v > 10;`,
		},
		{
			name:   "index-expression",
			prefix: "idx:drift_t1_v_expr",
			ddl:    `DROP INDEX drift_t1_v_expr; CREATE INDEX drift_t1_v_expr ON drift_t1 ((v + 2)) INCLUDE (id) WHERE v > 10;`,
		},
		{
			name:   "index-predicate",
			prefix: "idx:drift_t1_v_expr",
			ddl:    `DROP INDEX drift_t1_v_expr; CREATE INDEX drift_t1_v_expr ON drift_t1 ((v + 1)) INCLUDE (id) WHERE v > 50;`,
		},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			ctx := context.Background()
			apply := func(ctx context.Context, db *sql.DB) error {
				_, err := db.ExecContext(ctx, driftBaseDDL)
				return err
			}
			helperDB := postgresproof.OpenIsolatedSchema(t, dsn, "drift_7693", apply)
			referenceDB := postgresproof.OpenIsolatedSchema(t, dsn, "drift_7693ref", apply)

			if _, err := helperDB.ExecContext(ctx, mutation.ddl); err != nil {
				t.Fatalf("apply %s mutation: %v", mutation.name, err)
			}

			helperSchema := isolatedSchemaCurrentSchema(t, ctx, helperDB)
			referenceSchema := isolatedSchemaCurrentSchema(t, ctx, referenceDB)
			diff := diffIsolatedSchemaObjects(
				listIsolatedSchemaObjects(t, ctx, helperDB, helperSchema),
				listIsolatedSchemaObjects(t, ctx, referenceDB, referenceSchema),
			)
			if len(diff) == 0 {
				t.Fatalf("%s drift undetected: mutated schema compares identical to the reference", mutation.name)
			}
			for _, object := range diff {
				if strings.HasPrefix(object, mutation.prefix) {
					return
				}
			}
			t.Fatalf("%s drift reported %d object(s) but none for %q: %v", mutation.name, len(diff), mutation.prefix, diff)
		})
	}
}

// diffIsolatedSchemaObjects returns the symmetric difference of two schema
// inventories: every object present on exactly one side.
func diffIsolatedSchemaObjects(helperObjects, referenceObjects []string) []string {
	helperSet := make(map[string]struct{}, len(helperObjects))
	for _, object := range helperObjects {
		helperSet[object] = struct{}{}
	}
	referenceSet := make(map[string]struct{}, len(referenceObjects))
	for _, object := range referenceObjects {
		referenceSet[object] = struct{}{}
	}
	var diff []string
	for _, object := range referenceObjects {
		if _, ok := helperSet[object]; !ok {
			diff = append(diff, object)
		}
	}
	for _, object := range helperObjects {
		if _, ok := referenceSet[object]; !ok {
			diff = append(diff, object)
		}
	}
	return diff
}
