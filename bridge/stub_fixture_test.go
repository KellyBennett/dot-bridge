package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

type stubFixture struct {
	*taskFixture
	adapter  *StubAdapter
	worker   *Worker
	scenario string
	now      time.Time
}

func newStubFixture(t *testing.T, scenario string) *stubFixture {
	f := &stubFixture{taskFixture: newTaskFixture(t), scenario: scenario, now: fixtureTime()}
	f.setupStub()
	f.envelope.ProfileRevision = f.profile()
	return f
}
func (f *stubFixture) setupStub() {
	f.adapter = f.newAdapter()
	f.configureProfile()
	f.rebuild()
	f.worker = f.newWorker(f.adapter)
}
func (f *stubFixture) configureProfile() {
	f.config.Tasks.ProfileRevision = f.profile()
	f.config.Clock = func() time.Time { return f.now }
}
func (f *stubFixture) profile() string { return f.adapter.Capabilities().ProfileRevision }
func (f *stubFixture) newAdapter() *StubAdapter {
	adapter, err := NewStubAdapter(f.journal, f.scenario)
	if err != nil {
		f.t.Fatal(err)
	}
	return adapter
}
func (f *stubFixture) newWorker(adapter ExecutionAdapter) *Worker {
	worker, err := f.broker.NewWorker(adapter)
	if err != nil {
		f.t.Fatal(err)
	}
	return worker
}
func (f *stubFixture) tick() taskAssertion {
	response, err := f.rawTick(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	f.persisted(response.Receipt)
	return taskAssertion{t: f.t, response: response}
}
func (f *stubFixture) advance(duration time.Duration) { f.now = f.now.Add(duration) }
func (f *stubFixture) restartStub() {
	f.restart()
	f.setupStub()
}
func (f *stubFixture) status(id string) lifecycle {
	payload := f.storedPayload("run_lifecycle", id)
	run, err := decodeStatus(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	return lifecycle{run: run}
}
func (a taskAssertion) state(want string) RunData {
	a.t.Helper()
	if a.response.Run == nil || a.response.Run.State != want || !a.response.Run.Simulated || a.response.Run.Summary != Label {
		a.t.Fatalf("want %s, got %+v", want, a.response)
	}
	return *a.response.Run
}
func (a taskAssertion) reason(want string) {
	if a.response.Run == nil || a.response.Run.TerminalReason != want {
		a.t.Fatal("wrong terminal reason", a.response)
	}
}
func (f *stubFixture) starts(want int) {
	var count int
	if err := f.journal.db.QueryRow("SELECT COUNT(*) FROM stub_dispatches").Scan(&count); err != nil || count != want {
		f.t.Fatal("dispatch count differs", count, err)
	}
}
func (f *stubFixture) queued() RunData { return f.accepted(f.approve()).run() }
func (f *stubFixture) complete() RunData {
	f.queued()
	f.tick().state("running")
	f.advance(2 * time.Second)
	return f.tick().state("completed")
}
func (f *stubFixture) readStatus(id string) taskAssertion {
	return f.readRun(map[string]any{"run_id": id})
}
func (f *stubFixture) readArtifacts(id string, ids []string) taskAssertion {
	return f.readRun(map[string]any{"run_id": id, "artifact_ids": ids})
}
func (f *stubFixture) manifestDigest(id string) string {
	return byteDigest(testJSON(f.t, f.storedResult(id)))
}
func (f *stubFixture) storedResult(id string) AdapterResult {
	payload := f.storedPayload("run_results", id)
	var result AdapterResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		f.t.Fatal(err)
	}
	return result
}
func (f *stubFixture) storedPayload(table, id string) string {
	var payload string
	if err := f.journal.db.QueryRow("SELECT payload FROM "+table+" WHERE run_id=?", id).Scan(&payload); err != nil {
		f.t.Fatal(err)
	}
	return payload
}
func (f *stubFixture) rawTick(ctx context.Context) (Response, error) { return f.worker.Tick(ctx) }
func (f *stubFixture) seconds(count int)                             { f.advance(time.Duration(count) * time.Second) }

func (f *stubFixture) stateOf(id string) string { return f.status(id).run.State }
func (f *stubFixture) auditCall(call taskCall) {
	call.assertion(f.t)
	f.persisted(call.response.Receipt)
}

func (f *stubFixture) eventDigest(id string) string {
	rows, err := f.journal.db.Query("SELECT payload FROM run_events WHERE run_id=? ORDER BY sequence", id)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	return f.hashEventRows(rows)
}

func (f *stubFixture) hashEventRows(rows *sql.Rows) string {
	var payloads []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			f.t.Fatal(err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return byteDigest(testJSON(f.t, payloads))
}
