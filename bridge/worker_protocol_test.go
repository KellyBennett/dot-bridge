package bridge

import (
	"context"
	"strings"
	"testing"
	"time"
)

type resultFaultAdapter struct {
	ExecutionAdapter
	fault string
}

func (a resultFaultAdapter) Collect(ctx context.Context, token string) (AdapterResult, error) {
	result, err := a.ExecutionAdapter.Collect(ctx, token)
	result.fixtureFault(a.fault)
	return result, err
}
func (result *AdapterResult) fixtureFault(fault string) {
	result.Manifest.fixtureFault(fault)
	result.Artifacts[0].fixtureFault(fault)
}
func (manifest *ResultManifest) fixtureFault(fault string) {
	switch fault {
	case "digest":
		manifest.EnvelopeDigest = byteDigest([]byte("other"))
	case "control":
		manifest.Unresolved = []string{"\x1b[31m"}
	case "verification":
		manifest.Verification[0].OutputDigest = byteDigest([]byte("model claim"))
	}
}
func (artifact *ResultArtifact) fixtureFault(fault string) {
	switch fault {
	case "oversized":
		artifact.Text = strings.Repeat("x", MaxDraftBytes+1)
	case "unlabelled":
		artifact.Summary = "ordinary output"
	}
}
func TestInvalidAdapterResultsStayUncertain(t *testing.T) {
	for _, fault := range []string{"digest", "oversized", "control", "unlabelled", "verification"} {
		t.Run(fault, func(t *testing.T) {
			f := newStubFixture(t, "success")
			f.installResultFault(fault)
			f.uncertainResult()
		})
	}
}
func (f *stubFixture) installResultFault(fault string) {
	f.worker = f.newWorker(resultFaultAdapter{ExecutionAdapter: f.adapter, fault: fault})
}
func TestWorkerRejectsUnapprovedProfile(t *testing.T) {
	f := newStubFixture(t, "success")
	f.hostChange("profile")
	f.workerRejected()
	f.starts(0)
}
func (f *stubFixture) workerRejected() {
	if _, err := f.broker.NewWorker(f.adapter); err == nil {
		f.t.Fatal("unapproved adapter accepted")
	}
}

type advancingAdapter struct {
	ExecutionAdapter
	advance func()
}

func (a advancingAdapter) Preflight(ctx context.Context, task FrozenTask) (string, error) {
	status, err := a.ExecutionAdapter.Preflight(ctx, task)
	a.advance()
	return status, err
}
func TestStartRechecksAuthorityAfterPreflight(t *testing.T) {
	f := newStubFixture(t, "success")
	f.queued()
	f.installAdvancingPreflight(10 * time.Minute)
	result := f.tick()
	result.state("failed")
	result.reason("APPROVAL_STALE")
	f.starts(0)
}
func (f *stubFixture) installAdvancingPreflight(duration time.Duration) {
	f.worker = f.newWorker(advancingAdapter{ExecutionAdapter: f.adapter, advance: func() { f.advance(duration) }})
}
func TestCorruptStoredResultDoesNotExport(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.complete()
	f.fail("UPDATE run_results SET payload=json_set(payload, '$.manifest.summary', 'unlabelled')")
	f.failedRead(run)
}

func (f *stubFixture) uncertainResult() {
	f.queued()
	f.tick().state("running")
	f.seconds(2)
	result := f.tick()
	result.state("uncertain")
	result.reason("ADAPTER_PROTOCOL_ERROR")
}
