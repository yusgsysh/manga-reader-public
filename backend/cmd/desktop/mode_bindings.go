//go:build bindings

package main

// bindingsMode marks the throwaway binary Wails builds with `-tags bindings`
// to extract bound Go methods. It runs before the frontend is embedded and
// before any database or listener exists, so run() must only call wails.Run.
var bindingsMode = true
