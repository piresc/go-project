// Package migrations embeds the user-service SQL migrations.
package migrations

import "embed"

// FS holds the migration files.
//
//go:embed *.sql
var FS embed.FS
