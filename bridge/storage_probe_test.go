package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

// journalProbe verifies database evidence without participating in dispatch.
type journalProbe struct {
	t       *testing.T
	journal *Journal
}

func (p *journalProbe) queries() *store.Queries { return store.New(p.journal.db) }

func (p *journalProbe) lookup(id string) string {
	stored, err := p.queries().GetReceipt(context.Background(), id)
	if err != nil {
		p.t.Fatal(err)
	}
	return stored
}

func (p *journalProbe) sameReceipt(expected Receipt) {
	stored := p.lookup(expected.ReceiptID)
	var got Receipt
	if err := json.Unmarshal([]byte(stored), &got); err != nil || got != expected {
		p.t.Fatal("stored receipt differs", err)
	}
}

func (p *journalProbe) secretAbsent(id, secret string) {
	if strings.Contains(p.lookup(id), secret) {
		p.t.Fatal("fixture text leaked into journal")
	}
}

func (p *journalProbe) count(expected int) {
	var count int
	err := p.journal.db.QueryRow("SELECT COUNT(*) FROM receipts").Scan(&count)
	if err != nil || count != expected {
		p.t.Fatal("receipt count differs", count, err)
	}
}

func (p *journalProbe) migrated() {
	var version int
	err := p.journal.db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied").Scan(&version)
	if err != nil || version != 2 {
		p.t.Fatal("migration version not recorded", err)
	}
}

func (p *journalProbe) failUpdates() {
	p.exec(`CREATE TRIGGER reject_receipt BEFORE UPDATE ON receipts
		BEGIN SELECT RAISE(ABORT, 'disk failure fixture'); END`)
}

func (p *journalProbe) allowUpdates() { p.exec("DROP TRIGGER reject_receipt") }

func (p *journalProbe) exec(query string) {
	if _, err := p.journal.db.Exec(query); err != nil {
		p.t.Fatal(err)
	}
}

func (p *journalProbe) durability() {
	var mode int
	if err := p.journal.db.QueryRow("PRAGMA synchronous").Scan(&mode); err != nil || mode != 2 {
		p.t.Fatal("journal durability is not FULL", err)
	}
}

func (p *journalProbe) rejectsPayload(payload string) {
	if _, err := p.journal.db.Exec("INSERT INTO receipts(receipt_id,payload) VALUES (?,?)", "fixture", payload); err == nil {
		p.t.Fatal("invalid receipt accepted", payload)
	}
}

func (p *journalProbe) missing() {
	var payload string
	err := p.journal.db.QueryRow("SELECT payload FROM receipts WHERE receipt_id='missing'").Scan(&payload)
	if err != sql.ErrNoRows {
		p.t.Fatal("unexpected missing receipt behavior", err)
	}
}
