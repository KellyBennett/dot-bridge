package bridge

import (
	"context"
	"testing"
	"time"
)

func TestObservationWritesAndReceiptRollbackTogether(t *testing.T) {
	for _, target := range []string{"run_events", "run_results", "run_lifecycle", "receipts"} {
		t.Run(target, func(t *testing.T) {
			f := newStubFixture(t, "success")
			run := f.queued()
			f.tick().state("running")
			f.advance(2 * time.Second)
			f.failObservation(target)
			f.failedTick()
			f.noObservation(run.RunID)
		})
	}
}
func TestDispatchIntentFailureCannotStart(t *testing.T) {
	f := newStubFixture(t, "success")
	f.queued()
	f.failObservation("run_events")
	f.failedTick()
	f.starts(0)
}
func TestUncertainActiveRunBlocksNextQueuedTask(t *testing.T) {
	f := newStubFixture(t, "loss_after_start")
	f.queued()
	f.tick().state("running")
	f.useKey("00000000-0000-4000-8000-000000000020")
	next := f.queued()
	f.seconds(31)
	f.tick().state("uncertain")
	f.tick().state("uncertain")
	f.readStatus(next.RunID).state("accepted")
	f.starts(1)
}
func (f *stubFixture) failObservation(target string) {
	action, condition := observationFailureBoundary(target)
	f.fail("CREATE TRIGGER fail_observation BEFORE " + action + " ON " + target + condition + " BEGIN SELECT RAISE(ABORT, 'fixture'); END")
}
func observationFailureBoundary(target string) (string, string) {
	if target == "run_lifecycle" {
		return "UPDATE", " WHEN NEW.state='completed'"
	}
	if target == "receipts" {
		return "UPDATE", " WHEN json_extract(NEW.payload, '$.effect')='run_observed'"
	}
	return "INSERT", ""
}
func (f *stubFixture) failedTick() {
	response, err := f.worker.Tick(context.Background())
	requireNoTaskResponse(f.t, response, err)
}
func (f *stubFixture) noObservation(id string) {
	if f.stateOf(id) != "running" {
		f.t.Fatal("failed observation changed status")
	}
	var results int
	if err := f.journal.db.QueryRow("SELECT COUNT(*) FROM run_results").Scan(&results); err != nil || results != 0 {
		f.t.Fatal("partial result persisted", err)
	}
	var events int
	if err := f.journal.db.QueryRow("SELECT COUNT(*) FROM run_events").Scan(&events); err != nil || events != 2 {
		f.t.Fatal("partial events persisted", events, err)
	}
	f.starts(1)
}
