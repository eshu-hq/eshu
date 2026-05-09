package dbtsql

import (
	"reflect"
	"testing"
)

func TestExtractCompiledModelLineageCapturesMacroProjectionWithoutUnresolvedGap(t *testing.T) {
	t.Parallel()

	got := ExtractCompiledModelLineage(
		`select
  dbt_utils.identity(o.amount) as macro_amount
from raw.public.orders o`,
		"order_metrics",
		map[string][]string{
			"raw.public.orders": {"amount"},
		},
	)

	if got.ProjectionCount != 1 {
		t.Fatalf("ProjectionCount = %d, want 1", got.ProjectionCount)
	}
	if len(got.UnresolvedReferences) != 0 {
		t.Fatalf("UnresolvedReferences = %#v, want none", got.UnresolvedReferences)
	}

	want := []ColumnLineage{
		{
			OutputColumn:  "macro_amount",
			SourceColumns: []string{"raw.public.orders.amount"},
		},
	}
	if !reflect.DeepEqual(got.ColumnLineage, want) {
		t.Fatalf("ColumnLineage = %#v, want %#v", got.ColumnLineage, want)
	}
}

func TestExtractCompiledModelLineageCapturesWindowProjectionWithoutUnresolvedGap(t *testing.T) {
	t.Parallel()

	got := ExtractCompiledModelLineage(
		`select
  sum(o.amount) over (partition by o.customer_id order by o.order_date) as rolling_amount
from raw.public.orders o`,
		"order_metrics",
		map[string][]string{
			"raw.public.orders": {"amount", "customer_id", "order_date"},
		},
	)

	if got.ProjectionCount != 1 {
		t.Fatalf("ProjectionCount = %d, want 1", got.ProjectionCount)
	}
	if len(got.UnresolvedReferences) != 0 {
		t.Fatalf("UnresolvedReferences = %#v, want none", got.UnresolvedReferences)
	}

	want := []ColumnLineage{
		{
			OutputColumn: "rolling_amount",
			SourceColumns: []string{
				"raw.public.orders.amount",
				"raw.public.orders.customer_id",
				"raw.public.orders.order_date",
			},
			TransformKind:       "window_sum",
			TransformExpression: "sum(o.amount) over (partition by o.customer_id order by o.order_date)",
		},
	}
	if !reflect.DeepEqual(got.ColumnLineage, want) {
		t.Fatalf("ColumnLineage = %#v, want %#v", got.ColumnLineage, want)
	}
}

func TestExtractCompiledModelLineageCapturesNestedSafeWrapperWithoutUnresolvedGap(t *testing.T) {
	t.Parallel()

	got := ExtractCompiledModelLineage(
		`select
  upper(coalesce(c.segment, 'unknown')) as normalized_segment
from raw.public.customers c`,
		"order_metrics",
		map[string][]string{
			"raw.public.customers": {"segment"},
		},
	)

	if got.ProjectionCount != 1 {
		t.Fatalf("ProjectionCount = %d, want 1", got.ProjectionCount)
	}
	if len(got.UnresolvedReferences) != 0 {
		t.Fatalf("UnresolvedReferences = %#v, want none", got.UnresolvedReferences)
	}

	want := []ColumnLineage{
		{
			OutputColumn:        "normalized_segment",
			SourceColumns:       []string{"raw.public.customers.segment"},
			TransformKind:       "upper",
			TransformExpression: "upper(coalesce(c.segment, 'unknown'))",
		},
	}
	if !reflect.DeepEqual(got.ColumnLineage, want) {
		t.Fatalf("ColumnLineage = %#v, want %#v", got.ColumnLineage, want)
	}
}

func TestExtractCompiledModelLineageCapturesNestedWrapperOnCTEColumnWithoutUnresolvedGap(t *testing.T) {
	t.Parallel()

	got := ExtractCompiledModelLineage(
		`with customer_orders as (
  select
    c.full_name as source_customer_name
  from raw.public.customers c
)
select
  trim(lower(source_customer_name)) as normalized_customer_name
from customer_orders`,
		"order_metrics",
		map[string][]string{
			"raw.public.customers": {"full_name"},
		},
	)

	if got.ProjectionCount != 1 {
		t.Fatalf("ProjectionCount = %d, want 1", got.ProjectionCount)
	}
	if len(got.UnresolvedReferences) != 0 {
		t.Fatalf("UnresolvedReferences = %#v, want none", got.UnresolvedReferences)
	}

	want := []ColumnLineage{
		{
			OutputColumn:        "normalized_customer_name",
			SourceColumns:       []string{"raw.public.customers.full_name"},
			TransformKind:       "trim",
			TransformExpression: "trim(lower(source_customer_name))",
		},
	}
	if !reflect.DeepEqual(got.ColumnLineage, want) {
		t.Fatalf("ColumnLineage = %#v, want %#v", got.ColumnLineage, want)
	}
}
