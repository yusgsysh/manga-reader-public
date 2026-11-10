// Package schema declares the ent schemas for the app's tables.
//
// The files here are consumed by internal/ent/entc at code-generation time, not
// by the running binary: entc loads this package, type-asserts the optional
// Annotations/Indexes interfaces, and renders migrate/schema.go from them. Those
// methods — and helpers such as compositeID that only they call — are therefore
// invisible to reachability analysis (golang.org/x/tools/cmd/deadcode reports
// them as unreachable). They are required for `go generate ./internal/ent` and
// must not be deleted.
package schema
