// Package migrations embeds the goose SQL migrations so tests and tooling
// apply exactly the schema that production runs.
package migrations

import "embed"

// FS holds every *.sql migration file.
//
//go:embed *.sql
var FS embed.FS
