// Package migrations embeds the SQL migration files for both drivers so the
// binaries are self-contained (no external migration tooling required).
package migrations

import "embed"

// FS contains migrations/postgres/*.sql and migrations/sqlite/*.sql.
//
//go:embed postgres/*.sql sqlite/*.sql
var FS embed.FS
