package migrations

import "embed"

// Files contains the ordered, forward-only SQL migrations.
//
//go:embed *.up.sql
var Files embed.FS
