package gocmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_moveSliceElementsThatAreInMapToBack(t *testing.T) {
	type testCase struct {
		S              []string
		M              map[string]struct{}
		Expected       []string
		ExpectedLength int
	}
	for _, tc := range []testCase{
		{
			S: []string{"v1", "v2", "v3", "v4", "v5"},
			M: map[string]struct{}{
				"v1": {},
				"v3": {},
			},
			Expected:       []string{"v5", "v2", "v4", "v3", "v1"},
			ExpectedLength: 3,
		},
		{
			S: []string{"v1"},
			M: map[string]struct{}{
				"v1": {},
			},
			Expected:       []string{"v1"},
			ExpectedLength: 0,
		},
	} {
		actual := moveSliceElementsThatAreInMapToBack(tc.S, tc.M)
		actualResliced := actual[:cap(actual)]
		assert.Equal(t, tc.Expected, actualResliced)
		assert.Equal(t, tc.ExpectedLength, len(actual))
	}
}

func Test_swap(t *testing.T) {
	s := []string{"v1", "v2"}
	swap(s, 0, 1)
	assert.Equal(t, []string{"v2", "v1"}, s)
}
