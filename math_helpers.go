package nara

import "sort"

// TrimmedMeanInt64 calculates a robust average by removing outliers.
// It computes the median, then removes values outside [median*0.2, median*5.0],
// and returns the average of the remaining values.
//
// Special cases:
//   - Empty input: returns 0
//   - Single value: returns that value
//   - Zero median: keeps values in [-10, 10] range
//   - Negative median: inverts bounds appropriately
//   - All values filtered: falls back to median
func TrimmedMeanInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}

	// Sort
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Get median
	median := sorted[len(sorted)/2]

	// Filter outliers (keep values within 0.2x - 5x of median)
	// Handle edge cases: zero median, negative median
	var filtered []int64
	if median == 0 {
		// For zero median, keep values close to zero (within small range)
		for _, v := range sorted {
			if v >= -10 && v <= 10 {
				filtered = append(filtered, v)
			}
		}
	} else if median > 0 {
		// Positive median: standard 0.2x - 5x range
		lowerBound := median / 5
		upperBound := median * 5
		for _, v := range sorted {
			if v >= lowerBound && v <= upperBound {
				filtered = append(filtered, v)
			}
		}
	} else {
		// Negative median: invert bounds (5x is smaller, 0.2x is larger)
		lowerBound := median * 5 // More negative (smaller)
		upperBound := median / 5 // Less negative (larger)
		for _, v := range sorted {
			if v >= lowerBound && v <= upperBound {
				filtered = append(filtered, v)
			}
		}
	}

	if len(filtered) == 0 {
		return median
	}

	// Compute average
	var sum int64
	for _, v := range filtered {
		sum += v
	}
	return sum / int64(len(filtered))
}

// TrimmedMeanPositive calculates a robust average for positive values only.
// Zeros are filtered out before computing (they're considered "not real observations").
// Uses the same outlier removal logic as TrimmedMeanInt64.
func TrimmedMeanPositive(values []int64) int64 {
	// Filter out zeros first
	var nonZero []int64
	for _, v := range values {
		if v > 0 {
			nonZero = append(nonZero, v)
		}
	}
	return TrimmedMeanInt64(nonZero)
}
