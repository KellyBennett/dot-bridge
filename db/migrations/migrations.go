// Package migrations embeds the host-applied Goose schema migrations.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var Files embed.FS

// Up runs only trusted embedded schema migrations, never caller-supplied SQL.
func Up(ctx context.Context, db *sql.DB) error {
	provider, err := migrationProvider(db)
	if err != nil {
		return err
	}
	return applyProvider(ctx, provider)
}

func embeddedSource() fs.FS { return Files }

func migrationProvider(db *sql.DB) (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectSQLite3, db, embeddedSource())
}

func applyProvider(ctx context.Context, provider *goose.Provider) error {
	_, err := provider.Up(ctx)
	return err
}
