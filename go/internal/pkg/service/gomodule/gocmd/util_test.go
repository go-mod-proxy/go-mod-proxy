package gocmd

import (
	"testing"
)

func Test_LooksLikeGoogleVirtualPrivateCloudError_Test(t *testing.T) {
	errorString := `: reading https://proxy.golang.org/google.golang.org/api/@v/v0.8.0.zip: 403 Forbidden`
	x := looksLikeGoogleVirtualPrivateCloudError(errorString)
	if !x {
		t.Logf("error %#v", errorString)
		t.Fail()
	}
}
