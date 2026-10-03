package bridge

import (
	"context"
	"database/sql"
	"errors"
)

var ErrAuditUnavailable = errors.New("AUDIT_UNAVAILABLE: receipt unavailable")

type Journal struct{ db *sql.DB }

func OpenJournal(path string) (*Journal, error) {
	db, err := openDatabase(path)
	if err != nil {
		return nil, ErrAuditUnavailable
	}
	j := &Journal{db: db}
	if err = j.migrate(); err != nil {
		j.Close()
		return nil, ErrAuditUnavailable
	}
	return j, nil
}

func (j *Journal) Close() error { return j.db.Close() }

func (j *Journal) Record(ctx context.Context, receipt Receipt) (Receipt, error) {
	tx, err := j.begin(ctx)
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	defer tx.rollback()
	result, err := tx.store(receipt)
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	if err = tx.commit(); err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	return result, nil
}
