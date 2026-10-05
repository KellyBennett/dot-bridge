package bridge

import (
	"testing"
	"time"
)

func TestRunEventPaginationSurvivesRestart(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.complete()
	page := f.readStatus(run.RunID)
	page.eventPage(1, 100, true)
	f.restartStub()
	tail := f.readCursor(run.RunID, page.response.NextCursor)
	tail.eventPage(101, 25, false)
	f.seconds(1)
	f.readCursor(run.RunID, tail.response.NextCursor).eventPage(126, 0, false)
}
func TestCursorCannotCrossRuns(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.complete()
	page := f.readStatus(run.RunID)
	f.key = "00000000-0000-4000-8000-000000000020"
	other := f.queued()
	f.readCursor(other.RunID, page.response.NextCursor).denied("CURSOR_EXPIRED")
}
func TestPollingLimitPersistsAcrossRestart(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.queued()
	f.readStatus(run.RunID).state("accepted")
	f.readStatus(run.RunID).state("accepted")
	f.restartStub()
	f.readStatus(run.RunID).denied("LIMIT_EXCEEDED")
	f.advance(time.Second)
	f.readStatus(run.RunID).state("accepted")
	f.starts(0)
}
func TestResultArtifactsRequireExplicitSelectionAndExport(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.complete()
	f.readStatus(run.RunID).resultPage(false)
	f.readArtifacts(run.RunID, []string{"SIMULATED_patch"}).resultPage(true)
	f.advance(time.Second)
	f.denyExports()
	f.readArtifacts(run.RunID, []string{"SIMULATED_patch"}).denied("EXPORT_BLOCKED")
}
func TestResultReadinessAndUnknownArtifact(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.queued()
	f.readArtifacts(run.RunID, []string{"SIMULATED_patch"}).denied("RESULT_NOT_READY")
	f.tick().state("running")
	f.advance(2 * time.Second)
	f.tick().state("completed")
	f.readArtifacts(run.RunID, []string{"SIMULATED_unknown"}).denied("NOT_FOUND")
}
func (f *stubFixture) readCursor(id, cursor string) taskAssertion {
	return f.readRun(map[string]any{"run_id": id, "event_cursor": cursor})
}
func (a taskAssertion) eventPage(first int64, count int, truncated bool) {
	if len(a.response.Events) != count || a.response.Truncated != truncated {
		a.t.Fatal("wrong event page", a.response)
	}
	for i, event := range a.response.Events {
		if event.Sequence != first+int64(i) || !event.Simulated || !labelled(event.Summary) {
			a.t.Fatal("invalid event sequence", event)
		}
	}
}
func (a taskAssertion) resultPage(artifacts bool) {
	a.runExportEvidence()
	if a.response.Result == nil || !a.response.Result.Simulated {
		a.t.Fatal("missing simulated manifest")
	}
	if (len(a.response.Artifacts) > 0) != artifacts {
		a.t.Fatal("wrong artifact selection")
	}
	for _, artifact := range a.response.Artifacts {
		if !artifact.valid() {
			a.t.Fatal("invalid artifact")
		}
	}
}

func TestPollingLimitUsesRollingSecond(t *testing.T) {
	f := newStubFixture(t, "success")
	run := f.queued()
	f.readStatus(run.RunID).state("accepted")
	f.advance(900 * time.Millisecond)
	f.readStatus(run.RunID).state("accepted")
	f.advance(200 * time.Millisecond)
	f.readStatus(run.RunID).state("accepted")
	f.readStatus(run.RunID).denied("LIMIT_EXCEEDED")
	f.advance(800 * time.Millisecond)
	f.readStatus(run.RunID).state("accepted")
}

func (a taskAssertion) runExportEvidence() {
	raw, err := a.response.runExport()
	if err != nil || len(raw) != a.response.Receipt.ExportedBytes || byteDigest(raw) != a.response.Receipt.OutputDigest {
		a.t.Fatal("export receipt does not bind output")
	}
	if len(testJSON(a.t, a.response)) > MaxRunResponseBytes {
		a.t.Fatal("response exceeds bound")
	}
}
