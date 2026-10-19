//go:build !race

package app

// raceEnabled tells the timing tests that the race detector is slowing
// every frame many times over, so their budgets do not apply.
const raceEnabled = false
