package query_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/query"
)

// loadEcommerceModel loads the shared e-commerce postgres fixture and returns
// the single SemanticModel within it.
func loadEcommerceModel(t *testing.T) *model.SemanticModel {
	t.Helper()
	doc, err := model.ParseFile(filepath.Join("..", "model", "testdata", "ecommerce_postgres.yaml"))
	if err != nil {
		t.Fatalf("load ecommerce model: %v", err)
	}
	if len(doc.Models) == 0 {
		t.Fatal("load ecommerce model: no models in document")
	}
	return &doc.Models[0]
}

// loadQuery parses a query fixture file. Any error fails the test immediately.
func loadQuery(t *testing.T, file string) *query.Query {
	t.Helper()
	q, err := query.ParseFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", file, err)
	}
	return q
}

// expectNoErrors fails the test if errs is non-empty.
func expectNoErrors(t *testing.T, label string, errs []query.ValidationError) {
	t.Helper()
	if len(errs) != 0 {
		t.Errorf("%s: want no errors, got:\n%s", label, joinErrors(errs))
	}
}

// expectErrorContaining fails the test if none of the errors contain sub.
func expectErrorContaining(t *testing.T, label, sub string, errs []query.ValidationError) {
	t.Helper()
	if len(errs) == 0 {
		t.Fatalf("%s: want at least one error containing %q, got none", label, sub)
	}
	if !strings.Contains(joinErrors(errs), sub) {
		t.Errorf("%s: no error contains %q; errors:\n%s", label, sub, joinErrors(errs))
	}
}

func joinErrors(errs []query.ValidationError) string {
	sb := &strings.Builder{}
	for _, e := range errs {
		sb.WriteString("  - ")
		sb.WriteString(e.Error())
		sb.WriteString("\n")
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Valid queries — all should validate cleanly against the e-commerce model.
// ---------------------------------------------------------------------------

func TestValidateValidQueries(t *testing.T) {
	m := loadEcommerceModel(t)

	validFiles := []string{
		"simple.json",
		"by_status_filtered.json",
		"multi_metric.json",
		"with_having.json",
		"with_order_limit.json",
		"cross_dataset.json",
		"null_filter.json",
	}

	for _, file := range validFiles {
		t.Run(file, func(t *testing.T) {
			q := loadQuery(t, file)
			errs := query.Validate(q, m)
			expectNoErrors(t, file, errs)
		})
	}
}

// ---------------------------------------------------------------------------
// Table-driven invalid queries — each case expects a specific error substring.
// ---------------------------------------------------------------------------

func TestValidateInvalidRules(t *testing.T) {
	m := loadEcommerceModel(t)

	cases := []struct {
		name    string
		file    string
		wantSub string
	}{
		{
			name:    "unknown metric",
			file:    "invalid_unknown_metric.json",
			wantSub: "nonexistent_metric",
		},
		{
			name:    "unknown dimension field",
			file:    "invalid_unknown_dimension.json",
			wantSub: "nonexistent_column",
		},
		{
			name:    "unknown filter field",
			file:    "invalid_unknown_filter_field.json",
			wantSub: "nonexistent_column",
		},
		{
			name:    "invalid filter operator",
			file:    "invalid_bad_op.json",
			wantSub: "BETWEEN",
		},
		{
			name:    "empty selection",
			file:    "invalid_empty_selection.json",
			wantSub: "at least one",
		},
		{
			name:    "non-dimension field used as dimension",
			file:    "invalid_non_dimension_field.json",
			wantSub: "order_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := loadQuery(t, tc.file)
			errs := query.Validate(q, m)
			expectErrorContaining(t, tc.name, tc.wantSub, errs)
		})
	}
}

// ---------------------------------------------------------------------------
// Inline query construction tests — validate specific rules without file I/O.
// ---------------------------------------------------------------------------

func TestValidateRefFormat(t *testing.T) {
	m := loadEcommerceModel(t)

	cases := []struct {
		name    string
		metrics []string
		dims    []string
		wantSub string
	}{
		{
			name:    "metric ref missing dot",
			metrics: []string{"revenue"},
			wantSub: "invalid reference",
		},
		{
			name:    "metric ref unknown dataset",
			metrics: []string{"nonexistent_dataset.revenue"},
			wantSub: "nonexistent_dataset",
		},
		{
			name:    "dimension ref unknown dataset",
			dims:    []string{"nope.region"},
			metrics: []string{"orders.revenue"},
			wantSub: "nope",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &query.Query{Metrics: tc.metrics, Dimensions: tc.dims}
			errs := query.Validate(q, m)
			expectErrorContaining(t, tc.name, tc.wantSub, errs)
		})
	}
}

func TestValidateHavingRules(t *testing.T) {
	m := loadEcommerceModel(t)

	t.Run("invalid having metric", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			Having:  []query.Having{{Metric: "orders.no_such_metric", Op: ">", Value: 0}},
		}
		expectErrorContaining(t, "having unknown metric", "no_such_metric", query.Validate(q, m))
	})

	t.Run("invalid having operator", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			Having:  []query.Having{{Metric: "orders.revenue", Op: "LIKE", Value: 0}},
		}
		expectErrorContaining(t, "having bad op", "LIKE", query.Validate(q, m))
	})
}

func TestValidateOrderByRules(t *testing.T) {
	m := loadEcommerceModel(t)

	t.Run("invalid order_by direction", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			OrderBy: []query.OrderItem{{Field: "orders.revenue", Direction: "SIDEWAYS"}},
		}
		expectErrorContaining(t, "bad direction", "SIDEWAYS", query.Validate(q, m))
	})

	t.Run("valid empty direction defaults to ASC", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			OrderBy: []query.OrderItem{{Field: "orders.revenue", Direction: ""}},
		}
		expectNoErrors(t, "empty direction", query.Validate(q, m))
	})
}

func TestValidateLimitOffsetRules(t *testing.T) {
	m := loadEcommerceModel(t)
	neg := -1

	t.Run("negative offset", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Offset: &neg}
		expectErrorContaining(t, "negative offset", "offset", query.Validate(q, m))
	})

	t.Run("negative limit", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Limit: &neg}
		expectErrorContaining(t, "negative limit", "limit", query.Validate(q, m))
	})

	t.Run("valid limit and offset", func(t *testing.T) {
		ten, twenty := 10, 20
		q := &query.Query{Metrics: []string{"orders.revenue"}, Limit: &ten, Offset: &twenty}
		expectNoErrors(t, "limit+offset", query.Validate(q, m))
	})
}

func TestValidateOrderByNulls(t *testing.T) {
	m := loadEcommerceModel(t)

	t.Run("invalid nulls placement", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			OrderBy: []query.OrderItem{{Field: "orders.revenue", Nulls: "MIDDLE"}},
		}
		expectErrorContaining(t, "bad nulls", "MIDDLE", query.Validate(q, m))
	})

	t.Run("valid nulls placement", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			OrderBy: []query.OrderItem{{Field: "orders.revenue", Direction: "DESC", Nulls: "LAST"}},
		}
		expectNoErrors(t, "nulls last", query.Validate(q, m))
	})
}

func TestValidateOrFilter(t *testing.T) {
	m := loadEcommerceModel(t)

	t.Run("valid OR group", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			Filters: []query.Filter{{Or: []query.Filter{
				{Field: "orders.status", Op: "=", Value: []byte(`"complete"`)},
				{Field: "orders.status", Op: "=", Value: []byte(`"shipped"`)},
			}}},
		}
		expectNoErrors(t, "or group", query.Validate(q, m))
	})

	t.Run("nested OR rejected", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			Filters: []query.Filter{{Or: []query.Filter{
				{Or: []query.Filter{{Field: "orders.status", Op: "=", Value: []byte(`"x"`)}}},
			}}},
		}
		expectErrorContaining(t, "nested or", "nested OR", query.Validate(q, m))
	})

	t.Run("invalid field inside OR group", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.revenue"},
			Filters: []query.Filter{{Or: []query.Filter{
				{Field: "orders.nonexistent_col", Op: "=", Value: []byte(`"x"`)},
			}}},
		}
		expectErrorContaining(t, "bad field in or", "nonexistent_col", query.Validate(q, m))
	})
}

func TestValidateBigQueryModel(t *testing.T) {
	doc, err := model.ParseFile(filepath.Join("..", "model", "testdata", "ecommerce_bigquery.yaml"))
	if err != nil {
		t.Fatalf("load bigquery model: %v", err)
	}
	m := &doc.Models[0]

	// The same logical queries should validate cleanly against the BigQuery model
	// because it has the same dataset/field/metric names — only sources differ.
	for _, file := range []string{"simple.json", "cross_dataset.json"} {
		t.Run(file, func(t *testing.T) {
			q := loadQuery(t, file)
			expectNoErrors(t, file, query.Validate(q, m))
		})
	}
}
