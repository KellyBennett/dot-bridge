package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"time"

	"github.com/KellyBennett/dot-bridge/db/migrations"
	"github.com/KellyBennett/dot-bridge/internal/store"
	_ "modernc.org/sqlite"
)

// ErrAuditUnavailable means no durable receipt can be returned.
var ErrAuditUnavailable = errors.New("AUDIT_UNAVAILABLE: receipt unavailable")

// Journal holds a host-owned SQLite store. The host must protect its directory,
// permissions and backups. Public requests cannot choose a journal path.
type Journal struct {
	db *sql.DB
}

func OpenJournal(path string) (*Journal, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrAuditUnavailable
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	query := url.Values{}
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(ON)")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, ErrAuditUnavailable
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = migrations.Up(ctx, db); err != nil {
		db.Close()
		return nil, ErrAuditUnavailable
	}
	return &Journal{db: db}, nil
}

func (j *Journal) Close() error { return j.db.Close() }

// Record commits one receipt atomically, including its database sequence.
// This is a read-only foundation, not a mutation intent/commit protocol.
func (j *Journal) Record(ctx context.Context, receipt Receipt) (Receipt, error) {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	defer tx.Rollback()
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	queries := store.New(tx)
	sequence, err := queries.InsertReceipt(ctx, store.InsertReceiptParams{ReceiptID: receipt.ReceiptID, Payload: string(encoded)})
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	receipt.JournalSequence = sequence
	encoded, err = json.Marshal(receipt)
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	rows, err := queries.FinalizeReceipt(ctx, store.FinalizeReceiptParams{Payload: string(encoded), Sequence: sequence})
	if err != nil || rows != 1 {
		return Receipt{}, ErrAuditUnavailable
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	return receipt, nil
}
