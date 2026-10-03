package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

// receiptTransaction owns both sequence allocation and final payload persistence.
type receiptTransaction struct {
	ctx     context.Context
	tx      *sql.Tx
	queries *store.Queries
}

func (j *Journal) begin(ctx context.Context) (*receiptTransaction, error) {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return newReceiptTransaction(ctx, tx), nil
}

func newReceiptTransaction(ctx context.Context, tx *sql.Tx) *receiptTransaction {
	return &receiptTransaction{ctx: ctx, tx: tx, queries: store.New(tx)}
}

func (t *receiptTransaction) rollback()     { _ = t.tx.Rollback() }
func (t *receiptTransaction) commit() error { return t.tx.Commit() }

func (t *receiptTransaction) store(r Receipt) (Receipt, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return Receipt{}, err
	}
	sequence, err := t.insert(r.ReceiptID, string(payload))
	if err != nil {
		return Receipt{}, err
	}
	r.JournalSequence = sequence
	return t.finalize(r)
}

func (t *receiptTransaction) insert(id, payload string) (int64, error) {
	return t.queries.InsertReceipt(t.ctx, store.InsertReceiptParams{ReceiptID: id, Payload: payload})
}

func (t *receiptTransaction) finalize(r Receipt) (Receipt, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return Receipt{}, err
	}
	if err = t.update(string(payload), r.JournalSequence); err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func (t *receiptTransaction) update(payload string, sequence int64) error {
	rows, err := t.queries.FinalizeReceipt(t.ctx, store.FinalizeReceiptParams{Payload: payload, Sequence: sequence})
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("receipt finalization did not update one row")
	}
	return nil
}
