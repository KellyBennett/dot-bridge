package bridge

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"time"

	"github.com/KellyBennett/dot-bridge/db/migrations"
	_ "modernc.org/sqlite"
)

type journalLocation string

func (location journalLocation) uri() (string, error) {
	absolute, err := filepath.Abs(string(location))
	if err != nil {
		return "", err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	uri.RawQuery = durabilityPragmas().Encode()
	return uri.String(), nil
}

func durabilityPragmas() url.Values {
	return url.Values{"_pragma": []string{"synchronous(FULL)", "busy_timeout(5000)", "foreign_keys(ON)"}}
}

func openDatabase(path string) (*sql.DB, error) {
	dsn, err := journalLocation(path).uri()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func (j *Journal) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return j.applySchema(ctx)
}

func (j *Journal) applySchema(ctx context.Context) error {
	return migrations.Up(ctx, j.db)
}
