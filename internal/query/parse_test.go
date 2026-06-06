package query_test

import (
	"path/filepath"
	"testing"

	"github.com/vincentk1991/gavagai/internal/query"
)

func TestParseFileValidJSON(t *testing.T) {
	cases := []struct {
		file           string
		wantMetrics    int
		wantDimensions int
		wantFilters    int
		wantHaving     int
		wantOrderBy    int
		wantLimit      *int
	}{
		{
			file:           "simple.json",
			wantMetrics:    1,
			wantDimensions: 1,
			wantLimit:      intPtr(100),
		},
		{
			file:           "by_status_filtered.json",
			wantMetrics:    2,
			wantDimensions: 2,
			wantFilters:    2,
			wantOrderBy:    1,
			wantLimit:      intPtr(50),
		},
		{
			file:           "multi_metric.json",
			wantMetrics:    3,
			wantDimensions: 2,
			wantOrderBy:    2,
		},
		{
			file:           "with_having.json",
			wantMetrics:    1,
			wantDimensions: 1,
			wantHaving:     1,
			wantOrderBy:    1,
		},
		{
			file:           "with_order_limit.json",
			wantMetrics:    1,
			wantDimensions: 1,
			wantFilters:    1,
			wantOrderBy:    1,
			wantLimit:      intPtr(365),
		},
		{
			file:           "cross_dataset.json",
			wantMetrics:    2,
			wantDimensions: 2,
			wantFilters:    2,
			wantHaving:     1,
			wantOrderBy:    1,
			wantLimit:      intPtr(20),
		},
		{
			file:           "null_filter.json",
			wantMetrics:    1,
			wantDimensions: 1,
			wantFilters:    1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			q, err := query.ParseFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("ParseFile(%s): unexpected error: %v", tc.file, err)
			}

			if got := len(q.Metrics); got != tc.wantMetrics {
				t.Errorf("metrics: want %d, got %d", tc.wantMetrics, got)
			}
			if got := len(q.Dimensions); got != tc.wantDimensions {
				t.Errorf("dimensions: want %d, got %d", tc.wantDimensions, got)
			}
			if got := len(q.Filters); got != tc.wantFilters {
				t.Errorf("filters: want %d, got %d", tc.wantFilters, got)
			}
			if got := len(q.Having); got != tc.wantHaving {
				t.Errorf("having: want %d, got %d", tc.wantHaving, got)
			}
			if got := len(q.OrderBy); got != tc.wantOrderBy {
				t.Errorf("order_by: want %d, got %d", tc.wantOrderBy, got)
			}
			if tc.wantLimit == nil {
				if q.Limit != nil {
					t.Errorf("limit: want nil, got %d", *q.Limit)
				}
			} else {
				if q.Limit == nil {
					t.Errorf("limit: want %d, got nil", *tc.wantLimit)
				} else if *q.Limit != *tc.wantLimit {
					t.Errorf("limit: want %d, got %d", *tc.wantLimit, *q.Limit)
				}
			}
		})
	}
}

func TestParseFileStructuralFields(t *testing.T) {
	q, err := query.ParseFile(filepath.Join("testdata", "by_status_filtered.json"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if q.Metrics[0] != "orders.revenue" {
		t.Errorf("metrics[0]: want orders.revenue, got %q", q.Metrics[0])
	}
	if q.Filters[0].Op != "IN" {
		t.Errorf("filters[0].op: want IN, got %q", q.Filters[0].Op)
	}
	if q.Filters[0].Field != "orders.status" {
		t.Errorf("filters[0].field: want orders.status, got %q", q.Filters[0].Field)
	}
	// Value for IN should be non-nil (raw JSON array).
	if q.Filters[0].Value == nil {
		t.Errorf("filters[0].value: want non-nil for IN operator")
	}
	if q.OrderBy[0].Direction != "DESC" {
		t.Errorf("order_by[0].direction: want DESC, got %q", q.OrderBy[0].Direction)
	}
}

func TestParseFileNullFilter(t *testing.T) {
	q, err := query.ParseFile(filepath.Join("testdata", "null_filter.json"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if q.Filters[0].Op != "IS NOT NULL" {
		t.Errorf("filter op: want IS NOT NULL, got %q", q.Filters[0].Op)
	}
	// IS NOT NULL carries no value.
	if q.Filters[0].Value != nil {
		t.Errorf("IS NOT NULL filter: want nil value, got non-nil")
	}
}

func TestParseFileMissing(t *testing.T) {
	_, err := query.ParseFile(filepath.Join("testdata", "does_not_exist.json"))
	if err == nil {
		t.Fatal("ParseFile on missing path: want error, got nil")
	}
}

func TestParseFileMalformedJSON(t *testing.T) {
	_, err := query.Parse([]byte(`{metrics: broken`))
	if err == nil {
		t.Fatal("Parse malformed JSON: want error, got nil")
	}
}

func intPtr(n int) *int { return &n }
