package tests

import "testing"

// §10 — Window functions (all PENDING: window function IR not implemented)

func TestWindowRowNumber(t *testing.T) {
	pendingTest(t, "10", "row-number", "ROW_NUMBER() window function not yet expressible in query IR")
	_ = loadQueryRaw(t, "s10_row_number.json")
}

func TestWindowRank(t *testing.T) {
	pendingTest(t, "10", "rank", "RANK() window function not yet expressible in query IR")
	_ = loadQueryRaw(t, "s10_rank.json")
}

func TestWindowRunningSum(t *testing.T) {
	pendingTest(t, "10", "running-sum", "running SUM window function not yet expressible in query IR")
	_ = loadQueryRaw(t, "s10_running_sum.json")
}

func TestWindowMovingAverage(t *testing.T) {
	pendingTest(t, "10", "moving-average", "moving average window function not yet expressible in query IR")
	_ = loadQueryRaw(t, "s10_moving_average.json")
}

func TestWindowFilter(t *testing.T) {
	pendingTest(t, "10", "window-filter", "filtering on window function results not yet supported")
	_ = loadQueryRaw(t, "s10_window_filter.json")
}
