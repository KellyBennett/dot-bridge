package bridge

import (
	"context"
	"testing"
)

func (f *bridgeFixture) unavailable(ctx context.Context) {
	r, err := f.attempt(ctx)
	responseAssertion{t: f.t, response: r}.withheld(err)
}

func (f *bridgeFixture) attempt(ctx context.Context) (Response, error) {
	return f.broker.Dispatch(ctx, fixtureRequest(f.t, nil), fixtureIdentity())
}

func TestAuditFailureWithholdsReadAndRollsBack(t *testing.T) {
	f := newFixture(t)
	f.probe().failUpdates()
	f.unavailable(context.Background())
	f.probe().count(0)
	f.probe().allowUpdates()
	f.read(nil).allowed()
}

func TestCancelledContextFailsClosed(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.unavailable(ctx)
	f.probe().count(0)
}

func (f *bridgeFixture) rejectsDuplicate(receipt Receipt) {
	if _, err := f.journal.Record(context.Background(), receipt); err == nil {
		f.t.Fatal("duplicate receipt ID accepted")
	}
}

func TestReceiptUniqueIDAndJSONRoundTrip(t *testing.T) {
	f := newFixture(t)
	r := f.read(nil)
	f.rejectsDuplicate(r.receipt())
	f.probe().sameReceipt(r.receipt())
	f.probe().count(1)
	f.probe().missing()
}

func TestDatabaseEnforcesSyntheticReceipt(t *testing.T) {
	f := newFixture(t)
	for _, payload := range []string{`{}`, `[]`, `{"receipt_id":"fixture","simulated":false}`,
		`{"receipt_id":"fixture","simulated":1}`, `{"receipt_id":"other","simulated":true}`, `not-json`} {
		f.probe().rejectsPayload(payload)
	}
	f.probe().durability()
}

func TestRestartPreservesReceiptAndMigrationVersion(t *testing.T) {
	f := newFixture(t)
	first := f.read(nil).receipt()
	f.restart()
	f.read(nil).sequenceAfter(t, first.JournalSequence)
	f.probe().sameReceipt(first)
	f.probe().count(2)
	f.probe().migrated()
}

func (a responseAssertion) sequenceAfter(t *testing.T, sequence int64) {
	if a.response.Receipt.JournalSequence <= sequence {
		t.Fatal("journal sequence regressed")
	}
}
