package util

import (
	"math"
	"testing"
)

func Test_AddInt64(t *testing.T) {
	type testCase struct {
		X, Y int64
		Sum  int64
		OK   bool
	}
	for _, tc := range []testCase{
		{
			X: math.MaxInt64,
			Y: 1,
		},
		{
			X: math.MinInt64,
			Y: math.MinInt64,
		},
		{
			X:   math.MinInt64,
			Y:   0,
			Sum: math.MinInt64,
			OK:  true,
		},
		{
			X:   3,
			Y:   4,
			Sum: 7,
			OK:  true,
		},
	} {
		actual, ok := AddInt64(tc.X, tc.Y)
		if actual != tc.Sum || ok != tc.OK {
			t.Errorf("AddInt64(%d, %d) want (%d, %v) got (%d, %v)", tc.X, tc.Y, tc.Sum, tc.OK, actual, ok)
		}
	}
}

func Test_SumInt64(t *testing.T) {
	type testCase struct {
		X   []int64
		Sum int64
		OK  bool
	}
	for _, tc := range []testCase{
		{
			X: []int64{math.MaxInt64, 1},
		},
		{
			X: []int64{math.MinInt64, math.MinInt64},
		},
		{
			X:   []int64{math.MinInt64, 0},
			Sum: math.MinInt64,
			OK:  true,
		},
		{
			X:   []int64{3, 4},
			Sum: 7,
			OK:  true,
		},
		{
			Sum: 0,
			OK:  true,
		},
		{
			X:   []int64{9},
			Sum: 9,
			OK:  true,
		},
	} {
		actual, ok := SumInt64(tc.X...)
		if actual != tc.Sum || ok != tc.OK {
			t.Errorf("SumInt64(%#v...) want (%d, %v) got (%d, %v)", tc.X, tc.Sum, tc.OK, actual, ok)
		}
	}
}
