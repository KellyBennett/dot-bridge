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
	f.downRetainingReceipts()
	f.downRetainingReceipts()
	f.down()
	f.tableCount(0)
	f.up()
	f.tableCount(1)
}

func TestTaskMigrationPreservesExistingReceipt(t *testing.T) {
	f := newSchemaFixture(t)
	f.upTo(1)
	f.seedReceipt()
	f.up()
	f.receiptRetained()
	f.down()
	f.receiptRetained()
}
func (f *schemaFixture) upTo(version int64) {
	if _, err := f.provider.UpTo(context.Background(), version); err != nil {
		f.t.Fatal(err)
	}
}
func (f *schemaFixture) seedReceipt() {
	_, err := f.db.Exec(`INSERT INTO receipts(receipt_id,payload) VALUES ('legacy','{"receipt_id":"legacy","simulated":true}')`)
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *schemaFixture) receiptRetained() {
	var payload string
	if err := f.db.QueryRow("SELECT payload FROM receipts WHERE receipt_id='legacy'").Scan(&payload); err != nil {
		f.t.Fatal(err)
	}
	if payload != `{"receipt_id":"legacy","simulated":true}` {
		f.t.Fatal("legacy receipt changed")
	}
}

func (f *schemaFixture) downRetainingReceipts() { f.down(); f.tableCount(1) }

func TestLifecycleMigrationBackfillsAcceptedRuns(t *testing.T) {
	f := newSchemaFixture(t)
	f.upTo(2)
	f.seedAcceptedRun()
	f.up()
	f.lifecycleRetained()
	f.down()
	f.up()
	f.lifecycleRetained()
}
func (f *schemaFixture) seedAcceptedRun() {
	f.exec(`INSERT INTO approvals VALUES('approval','principal','personal','project','digest','{"envelope_digest":"digest","simulated":true}','2099-01-01T00:00:00Z','sim_legacy')`)
	f.exec(`INSERT INTO task_runs VALUES('sim_legacy','principal','personal','project','submit_task','key','input','approval','sim_token','{"run_id":"sim_legacy","state":"accepted","state_version":1,"simulated":true}')`)
}
func (f *schemaFixture) exec(query string) {
	if _, err := f.db.Exec(query); err != nil {
		f.t.Fatal(err)
	}
}
func (f *schemaFixture) lifecycleRetained() {
	var state, phase string
	var version int
	err := f.db.QueryRow("SELECT state,phase,state_version FROM run_lifecycle WHERE run_id='sim_legacy'").Scan(&state, &phase, &version)
	if err != nil || state != "accepted" || phase != "queued" || version != 1 {
		f.t.Fatal("accepted run not backfilled", err)
	}
}
