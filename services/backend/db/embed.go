// Package db embeds the SQL migrations so binaries can ship them.
package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
