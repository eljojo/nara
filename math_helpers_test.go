package nara

import "testing"

func TestTrimmedMeanInt64_Empty(t *testing.T) {
	result := TrimmedMeanInt64([]int64{})
	if result != 0 {
		t.Errorf("Expected 0 for empty slice, got %d", result)
	}
}

func TestTrimmedMeanInt64_SingleValue(t *testing.T) {
	result := TrimmedMeanInt64([]int64{42})
	if result != 42 {
		t.Errorf("Expected 42 for single value, got %d", result)
	}
}

func TestTrimmedMeanInt64_RemovesOutliers(t *testing.T) {
	// Values: 100, 100, 100, 100, 1000000 (outlier)
	// Median: 100, so outlier (1000000 > 500) gets filtered
	result := TrimmedMeanInt64([]int64{100, 100, 100, 100, 1000000})
	if result != 100 {
		t.Errorf("Expected 100 (outlier removed), got %d", result)
	}
}

func TestTrimmedMeanInt64_NormalValues(t *testing.T) {
	// All values within range, should average normally
	result := TrimmedMeanInt64([]int64{10, 12, 11, 13, 14})
	// Median: 12, bounds: [2, 60], all pass, avg = 60/5 = 12
	if result != 12 {
		t.Errorf("Expected 12, got %d", result)
	}
}

func TestTrimmedMeanInt64_ZeroMedian(t *testing.T) {
	// When median is 0, keep values in [-10, 10]
	result := TrimmedMeanInt64([]int64{0, 0, 0, 5, -5, 100})
	// 100 should be filtered out
	// avg of 0, 0, 0, 5, -5 = 0
	if result != 0 {
		t.Errorf("Expected 0 for zero median case, got %d", result)
	}
}

func TestTrimmedMeanInt64_NegativeMedian(t *testing.T) {
	// Negative values
	result := TrimmedMeanInt64([]int64{-10, -10, -10, -12, -8})
	// Median: -10, bounds: [-50, -2]
	// All values in range, avg = -50/5 = -10
	if result != -10 {
		t.Errorf("Expected -10 for negative values, got %d", result)
	}
}

func TestTrimmedMeanPositive_FiltersZeros(t *testing.T) {
	// Zeros should be filtered before computing
	result := TrimmedMeanPositive([]int64{0, 0, 100, 100, 100})
	if result != 100 {
		t.Errorf("Expected 100 (zeros filtered), got %d", result)
	}
}

func TestTrimmedMeanPositive_AllZeros(t *testing.T) {
	result := TrimmedMeanPositive([]int64{0, 0, 0})
	if result != 0 {
		t.Errorf("Expected 0 for all zeros, got %d", result)
	}
}
