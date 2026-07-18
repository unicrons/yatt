// Package migrations embeds the schema migrations and applies them.
//
// Migrations live in one directory per database engine from day one. The
// SQLite set is the only one that exists today, but the phase-2 Postgres set
// drops in beside it without changing this runner or any caller.
package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

// FS holds the embedded migration files, one subdirectory per engine.
//
//go:embed sqlite/*.sql
var FS embed.FS

// Dialects maps a goose dialect to the embedded directory holding its
// migrations.
var Dialects = map[string]string{
	"sqlite": "sqlite",
}

// Up applies every pending migration for the given goose dialect.
//
// goose keeps package-level state (the base filesystem, the dialect and the
// logger), so it is configured and restored around each call rather than at
// init time. The logger is silenced because goose writes to stdout by default,
// which would contaminate the machine-readable output formats.
func Up(db *sql.DB, dialect string) error {
	dir, ok := Dialects[dialect]
	if !ok {
		return fmt.Errorf("migrations: no migration set for dialect %q", dialect)
	}

	goose.SetBaseFS(FS)
	defer goose.SetBaseFS(nil)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("migrations: applying %s migrations: %w", dialect, err)
	}
	return nil
}
