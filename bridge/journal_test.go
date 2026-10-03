package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/KellyBennett/dot-bridge/db/migrations"
	"github.com/pressly/goose/v3"
)

func TestRestartPreservesReceiptAndMigrationVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.sqlite3")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	first := dispatch(t, testBroker(t, j, fixtureConfig()), fixtureRequest(t, nil), fixtureIdentity())
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	second := dispatch(t, testBroker(t, j, fixtureConfig()), fixtureRequest(t, nil), fixtureIdentity())
	if second.Receipt.JournalSequence <= first.Receipt.JournalSequence {
		t.Fatal("journal sequence regressed after restart")
	}
	var count int
	if err = j.db.QueryRow("SELECT COUNT(*) FROM receipts").Scan(&count); err != nil || count != 2 {
		t.Fatal("receipt history lost", err)
	}
	var version int
	if err = j.db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil || version != 1 {
		t.Fatal("Goose migration not recorded", err)
	}
}

func TestConcurrentConnectionsCommitUniqueReceipts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.sqlite3")
	brokers := make([]*Broker, 4)
	for i := range brokers {
		j, err := OpenJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		defer j.Close()
		brokers[i] = testBroker(t, j, fixtureConfig())
	}
	type result struct {
		response Response
		err      error
	}
	results := make(chan result, 12)
	raw := fixtureRequest(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(b *Broker) {
			defer wg.Done()
			r, err := b.Dispatch(context.Background(), raw, fixtureIdentity())
			results <- result{r, err}
		}(brokers[i%len(brokers)])
	}
	wg.Wait()
	close(results)
	sequences := make([]int, 0, 12)
	for result := range results {
		if result.err != nil || result.response.Data == nil {
			t.Fatal("concurrent read failed", result.err)
		}
		sequences = append(sequences, int(result.response.Receipt.JournalSequence))
	}
	sort.Ints(sequences)
	for i, sequence := range sequences {
		if sequence != i+1 {
			t.Fatal("duplicate or missing sequence", sequences)
		}
	}
}

func TestGooseMigrationDownUp(t *testing.T) {
	j := testJournal(t)
	provider, err := goose.NewProvider(goose.DialectSQLite3, j.db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec("SELECT sequence FROM receipts"); err == nil {
		t.Fatal("Down did not remove table")
	}
	if err = migrations.Up(context.Background(), j.db); err != nil {
		t.Fatal(err)
	}
	if dispatch(t, testBroker(t, j, fixtureConfig()), fixtureRequest(t, nil), fixtureIdentity()).Data == nil {
		t.Fatal("Up did not recreate journal")
	}
}

func TestDatabaseEnforcesSyntheticReceipt(t *testing.T) {
	j := testJournal(t)
	for _, payload := range []string{`{}`, `[]`, `{"receipt_id":"fixture","simulated":false}`,
		`{"receipt_id":"fixture","simulated":1}`,
		`{"receipt_id":"other","simulated":true}`, `not-json`} {
		if _, err := j.db.Exec("INSERT INTO receipts(receipt_id,payload) VALUES (?,?)", "fixture", payload); err == nil {
			t.Fatal("invalid receipt accepted", payload)
		}
	}
	var synchronous int
	if err := j.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil || synchronous != 2 {
		t.Fatal("journal is not configured for FULL durability", err)
	}
}

func TestReceiptUniqueIDAndJSONRoundTrip(t *testing.T) {
	j := testJournal(t)
	b := testBroker(t, j, fixtureConfig())
	r := dispatch(t, b, fixtureRequest(t, nil), fixtureIdentity())
	if _, err := j.Record(context.Background(), r.Receipt); err == nil {
		t.Fatal("duplicate receipt ID accepted")
	}
	var payload string
	if err := j.db.QueryRow("SELECT payload FROM receipts WHERE receipt_id=?", r.Receipt.ReceiptID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var got Receipt
	if err := json.Unmarshal([]byte(payload), &got); err != nil || got != r.Receipt {
		t.Fatal("round trip changed receipt", err)
	}
	var missing string
	if err := j.db.QueryRow("SELECT payload FROM receipts WHERE receipt_id='missing'").Scan(&missing); err != sql.ErrNoRows {
		t.Fatal("unexpected missing row behavior", err)
	}
}
