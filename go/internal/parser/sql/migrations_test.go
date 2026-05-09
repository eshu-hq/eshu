package sql

import "testing"

func TestDetectSQLMigrationTool(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "flyway",
			path: "/repo/sql/V42__backfill_orders.sql",
			want: "flyway",
		},
		{
			name: "prisma",
			path: "/repo/prisma/migrations/20260414_add_orders/migration.sql",
			want: "prisma",
		},
		{
			name: "liquibase changelog",
			path: "/repo/liquibase/changelog/20260414_add_orders.sql",
			want: "liquibase",
		},
		{
			name: "golang migrate",
			path: "/repo/migrations/202604140101_add_orders.up.sql",
			want: "golang-migrate",
		},
		{
			name: "generic migrations",
			path: "/repo/migrations/20260414_add_orders.sql",
			want: "generic",
		},
		{
			name: "unknown",
			path: "/repo/sql/orders.sql",
			want: "",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := detectSQLMigrationTool(testCase.path); got != testCase.want {
				t.Fatalf("detectSQLMigrationTool(%q) = %q, want %q", testCase.path, got, testCase.want)
			}
		})
	}
}
