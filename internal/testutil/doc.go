// Package testutil provides shared test helpers. Tests using SetConfig,
// SwapVar or ChdirRepoRoot mutate global state and must not call t.Parallel().
package testutil
