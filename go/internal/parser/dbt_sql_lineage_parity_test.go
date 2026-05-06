package parser

import (
	"reflect"
	"testing"
)

func TestExtractCompiledModelLineage_ParitySupportedTransforms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		relations map[string][]string
		want      []ColumnLineage
	}{
		{
			name: "cast",
			sql: `select
  cast(o.amount as numeric) as normalized_amount
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"amount"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "normalized_amount",
					SourceColumns:       []string{"raw.public.orders.amount"},
					TransformKind:       "cast",
					TransformExpression: "cast(o.amount as numeric)",
				},
			},
		},
		{
			name: "date_trunc",
			sql: `select
  date_trunc('day', o.created_at) as created_day
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"created_at"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "created_day",
					SourceColumns:       []string{"raw.public.orders.created_at"},
					TransformKind:       "date_trunc",
					TransformExpression: "date_trunc('day', o.created_at)",
				},
			},
		},
		{
			name: "concat",
			sql: `select
  concat(c.first_name, c.last_name) as full_name
from raw.public.customers c`,
			relations: map[string][]string{
				"raw.public.customers": {"first_name", "last_name"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "full_name",
					SourceColumns:       []string{"raw.public.customers.first_name", "raw.public.customers.last_name"},
					TransformKind:       "concat",
					TransformExpression: "concat(c.first_name, c.last_name)",
				},
			},
		},
		{
			name: "upper over lineage-preserving macro",
			sql: `select
  upper(dbt_utils.identity(o.amount)) as normalized_amount
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"amount"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "normalized_amount",
					SourceColumns:       []string{"raw.public.orders.amount"},
					TransformKind:       "upper",
					TransformExpression: "upper(dbt_utils.identity(o.amount))",
				},
			},
		},
		{
			name: "md5",
			sql: `select
  md5(o.id) as hashed_order_id
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"id"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "hashed_order_id",
					SourceColumns:       []string{"raw.public.orders.id"},
					TransformKind:       "md5",
					TransformExpression: "md5(o.id)",
				},
			},
		},
		{
			name: "concat_ws",
			sql: `select
  concat_ws('-', c.first_name, c.last_name) as full_name
from raw.public.customers c`,
			relations: map[string][]string{
				"raw.public.customers": {"first_name", "last_name"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "full_name",
					SourceColumns:       []string{"raw.public.customers.first_name", "raw.public.customers.last_name"},
					TransformKind:       "concat_ws",
					TransformExpression: "concat_ws('-', c.first_name, c.last_name)",
				},
			},
		},
		{
			name: "macro-heavy wrapper",
			sql: `select
  dbt_utils.generate_surrogate_key(md5(o.id)) as surrogate_key
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"id"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:  "surrogate_key",
					SourceColumns: []string{"raw.public.orders.id"},
				},
			},
		},
		{
			name: "templated macro wrapper",
			sql: `select
  {{ dbt_utils.generate_surrogate_key(md5(o.id)) }} as surrogate_key
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"id"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:  "surrogate_key",
					SourceColumns: []string{"raw.public.orders.id"},
				},
			},
		},
		{
			name: "case with multiple sources",
			sql: `select
  case when o.amount > 100 then c.segment else 'standard' end as customer_segment
from raw.public.orders o
join raw.public.customers c on c.id = o.customer_id`,
			relations: map[string][]string{
				"raw.public.orders":    {"amount", "customer_id"},
				"raw.public.customers": {"id", "segment"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "customer_segment",
					SourceColumns:       []string{"raw.public.orders.amount", "raw.public.customers.segment"},
					TransformKind:       "case",
					TransformExpression: "case when o.amount > 100 then c.segment else 'standard' end",
				},
			},
		},
		{
			name: "arithmetic with multiple sources",
			sql: `select
  o.amount - p.discount as net_amount
from raw.public.orders o
join raw.public.payments p on p.order_id = o.id`,
			relations: map[string][]string{
				"raw.public.orders":   {"id", "amount"},
				"raw.public.payments": {"order_id", "discount"},
			},
			want: []ColumnLineage{
				{
					OutputColumn:        "net_amount",
					SourceColumns:       []string{"raw.public.orders.amount", "raw.public.payments.discount"},
					TransformKind:       "arithmetic",
					TransformExpression: "o.amount - p.discount",
				},
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := extractCompiledModelLineage(testCase.sql, "order_metrics", testCase.relations)
			if got.ProjectionCount != 1 {
				t.Fatalf("ProjectionCount = %d, want 1", got.ProjectionCount)
			}
			if len(got.UnresolvedReferences) != 0 {
				t.Fatalf("UnresolvedReferences = %#v, want none", got.UnresolvedReferences)
			}
			if !reflect.DeepEqual(got.ColumnLineage, testCase.want) {
				t.Fatalf("ColumnLineage = %#v, want %#v", got.ColumnLineage, testCase.want)
			}
		})
	}
}

func TestExtractCompiledModelLineage_ParityUnresolvedReasons(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		relations  map[string][]string
		expression string
		reason     string
	}{
		{
			name: "templated expression",
			sql: `select
  {{ unresolved_projection }} as projected_columns
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"id"},
			},
			expression: "{{ unresolved_projection }}",
			reason:     dbtTemplatedExpressionReason,
		},
		{
			name: "opaque macro",
			sql: `select
  dbt_utils.generate_surrogate_key(md5('static-value')) as surrogate_key
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"id"},
			},
			expression: "dbt_utils.generate_surrogate_key(md5('static-value'))",
			reason:     dbtMacroExpressionReason,
		},
		{
			name: "templated opaque macro",
			sql: `select
  {{ dbt_utils.generate_surrogate_key(md5('static-value')) }} as surrogate_key
from raw.public.orders o`,
			relations: map[string][]string{
				"raw.public.orders": {"id"},
			},
			expression: "{{ dbt_utils.generate_surrogate_key(md5('static-value')) }}",
			reason:     dbtTemplatedExpressionReason,
		},
		{
			name: "ambiguous unqualified reference",
			sql: `select
  id as record_id
from raw.public.orders o
join raw.public.customers c on c.id = o.customer_id`,
			relations: map[string][]string{
				"raw.public.orders":    {"id", "customer_id"},
				"raw.public.customers": {"id"},
			},
			expression: "id",
			reason:     "unqualified_column_reference_ambiguous",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := extractCompiledModelLineage(testCase.sql, "order_metrics", testCase.relations)
			if got.ProjectionCount != 1 {
				t.Fatalf("ProjectionCount = %d, want 1", got.ProjectionCount)
			}
			if len(got.ColumnLineage) != 0 {
				t.Fatalf("ColumnLineage = %#v, want none", got.ColumnLineage)
			}
			if len(got.UnresolvedReferences) != 1 {
				t.Fatalf("UnresolvedReferences = %#v, want one unresolved reference", got.UnresolvedReferences)
			}
			want := map[string]string{
				"expression": testCase.expression,
				"model_name": "order_metrics",
				"reason":     testCase.reason,
			}
			if !reflect.DeepEqual(got.UnresolvedReferences[0], want) {
				t.Fatalf("UnresolvedReferences[0] = %#v, want %#v", got.UnresolvedReferences[0], want)
			}
		})
	}
}
