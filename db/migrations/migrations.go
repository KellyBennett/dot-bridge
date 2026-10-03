// Package migrations embeds the host-applied Goose schema migrations.
package migrations

import (
	"context"
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var Files embed.FS

// Up runs only trusted embedded schema migrations, never caller-supplied SQL.
func Up(ctx context.Context, db *sql.DB) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, Files)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
