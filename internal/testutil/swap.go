package testutil

import "testing"

// SwapVar replaces *p with v for the test's duration; restored on cleanup.
func SwapVar[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}
