// Package db embeds the SQL files into the binary, so the image needs no
// migration tool and no mounted files.
package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS

//go:embed fixtures/*.sql
var Fixtures embed.FS
