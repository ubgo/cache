package cachetest_test

import (
	"testing"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

// The Mock is the reference implementation: it must pass the same conformance
// suite every real adapter runs.
func TestMockConformance(t *testing.T) {
	cachetest.Run(t, func(_ *testing.T) cache.Cache {
		return cachetest.NewMock()
	})
}
