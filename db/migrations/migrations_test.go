package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

type schemaFixture struct {
	t        *testing.T
	db       *sql.DB
	provider *goose.Provider
}

func newSchemaFixture(t *testing.T) *schemaFixture {
	f := &schemaFixture{t: t}
	f.open(t.TempDir())
	t.Cleanup(func() { _ = f.db.Close() })
	return f
}

func (f *schemaFixture) open(dir string) {
	var err error
	f.db, err = sql.Open("sqlite", filepath.Join(dir, "schema.sqlite3"))
	if err != nil {
		f.t.Fatal(err)
	}
	f.provider, err = migrationProvider(f.db)
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *schemaFixture) up() {
	if err := Up(context.Background(), f.db); err != nil {
		f.t.Fatal(err)
	}
}

func (f *schemaFixture) down() {
	if _, err := f.provider.Down(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *schemaFixture) tableCount(expected int) {
	var count int
	err := f.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='receipts'").Scan(&count)
	if err != nil || count != expected {
		f.t.Fatal("receipt table count differs", count, err)
	}
}

func TestGooseMigrationDownUp(t *testing.T) {
	f := newSchemaFixture(t)
	f.up()
	f.tableCount(1)
	f.down()
	f.tableCount(0)
	f.up()
	f.tableCount(1)
}
