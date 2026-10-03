package bridge

import (
	"context"
	"testing"
	"time"
)

func TestLostStartAcknowledgementReconcilesSameToken(t *testing.T) {
	f := newStubFixture(t, "lost_ack")
	run := f.queued()
	f.tick().state("uncertain")
	f.starts(1)
	f.restartStub()
	f.tick().state("running")
	f.advance(2 * time.Second)
	f.tick().state("completed")
	f.readStatus(run.RunID).state("completed")
	f.starts(1)
}
func TestCrashAfterStartBeforeJournalUpdateRecovers(t *testing.T) {
	f := newStubFixture(t, "success")
	f.queued()
	f.cancelAfterStart()
	f.starts(1)
	f.advance(31 * time.Second)
	f.restartStub()
	f.tick().state("completed")
	f.starts(1)
}
func TestCrashBeforeStartRemainsUncertainWithoutRedispatch(t *testing.T) {
	f := newStubFixture(t, "success")
	f.queued()
	f.cancelBeforeStart()
	f.starts(0)
	f.advance(31 * time.Second)
	f.restartStub()
	f.tick().state("uncertain")
	f.tick().state("uncertain")
	f.starts(0)
}
func TestAdapterLossPreservesLastConfirmedState(t *testing.T) {
	f := newStubFixture(t, "loss_after_start")
	f.queued()
	f.tick().state("running")
	f.advance(2 * time.Second)
	f.tick().state("running")
	f.advance(30 * time.Second)
	result := f.tick()
	result.confirmedState("running")
	f.starts(1)
}
func TestMalformedEventsDoNotComplete(t *testing.T) {
	f := newStubFixture(t, "malformed_events")
	f.queued()
	f.tick().state("running")
	f.advance(2 * time.Second)
	result := f.tick()
	result.state("uncertain")
	result.reason("ADAPTER_PROTOCOL_ERROR")
	f.starts(1)
}
func TestMissingManifestDoesNotComplete(t *testing.T) {
	f := newStubFixture(t, "missing_manifest")
	f.queued()
	f.tick().state("running")
	f.advance(2 * time.Second)
	result := f.tick()
	run := result.state("uncertain")
	if run.ResultAvailable || run.LastConfirmedState != "completed" {
		t.Fatal("missing manifest reported completion")
	}
	result.reason("RESULT_NOT_READY")
}

type cancellingAdapter struct {
	ExecutionAdapter
	cancel context.CancelFunc
	before bool
}

func (a cancellingAdapter) Preflight(ctx context.Context, task FrozenTask) (string, error) {
	if a.before {
		a.cancel()
	}
	return a.ExecutionAdapter.Preflight(ctx, task)
}
func (a cancellingAdapter) Start(ctx context.Context, input DispatchInput) (StartObservation, error) {
	result, err := a.ExecutionAdapter.Start(ctx, input)
	a.cancel()
	return result, err
}
func (f *stubFixture) cancelBeforeStart() { f.cancelledTick(true) }
func (f *stubFixture) cancelAfterStart()  { f.cancelledTick(false) }
func (f *stubFixture) cancelledTick(before bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.installCancellingAdapter(cancel, before)
	response, err := f.rawTick(ctx)
	requireNoTaskResponse(f.t, response, err)
}

func (a taskAssertion) confirmedState(want string) {
	run := a.state("uncertain")
	if run.LastConfirmedState != want {
		a.t.Fatal("lost last confirmed state")
	}
}

func (f *stubFixture) installCancellingAdapter(cancel context.CancelFunc, before bool) {
	f.worker = f.newWorker(cancellingAdapter{ExecutionAdapter: f.adapter, cancel: cancel, before: before})
}
