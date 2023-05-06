package util

import (
	"math"
)

// AddInt64 adds two signed 64-bit integers with overflow detection.
func AddInt64(x, y int64) (sum int64, ok bool) {
	if x > 0 && y > math.MaxInt64-x {
		return
	}
	if x < 0 && y < math.MinInt64-x {
		return
	}
	sum = x + y
	ok = true
	return
}

// SumInt64 sums multiple signed 64-bit integers with overflow detection.
func SumInt64(x ...int64) (sum int64, ok bool) {
	ok = true
	if len(x) > 0 {
		sum = x[0]
		for i := 1; i < len(x); i++ {
			sum, ok = AddInt64(sum, x[i])
			if !ok {
				return
			}
		}
	}
	return
}
