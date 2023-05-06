package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Environ(t *testing.T) {

	t.Run("Unset", func(t *testing.T) {
		t.Run("SuccessFoundCaseInsensitive", func(t *testing.T) {
			e := &Environ{
				environ: []string{
					"ENV1=V1",
					"ENV2=V2",
				},
				index: map[string]int{
					"env1": 0,
					"env2": 1,
				},
			}
			e.Unset("env1")
			assert.Equal(t, []string{"ENV2=V2"}, e.environ)
			assert.Equal(t, map[string]int{
				"env2": 0,
			}, e.index)
		})
		t.Run("SuccessFoundCaseSensitive", func(t *testing.T) {
			e := &Environ{
				environ: []string{
					"ENV1=V1",
					"ENV2=V2",
					"ENV3=V3",
				},
				isCaseSensitive: true,
				index: map[string]int{
					"ENV1": 0,
					"ENV2": 1,
					"ENV3": 2,
				},
			}
			e.Unset("ENV2")
			assert.Equal(t, []string{"ENV1=V1", "ENV3=V3"}, e.environ)
			assert.Equal(t, map[string]int{
				"ENV1": 0,
				"ENV3": 1,
			}, e.index)
		})
		t.Run("SuccessNotFound", func(t *testing.T) {
			e := &Environ{
				environ: []string{
					"ENV1=V1",
					"ENV2=V2",
				},
				isCaseSensitive: true,
				index: map[string]int{
					"ENV1": 0,
					"ENV2": 1,
				},
			}
			e.Unset("ENV3")
			assert.Equal(t, []string{"ENV1=V1", "ENV2=V2"}, e.environ)
			assert.Equal(t, map[string]int{
				"ENV1": 0,
				"ENV2": 1,
			}, e.index)
		})
	})

}
