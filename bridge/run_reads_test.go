package bridge

import "testing"

func TestRunLookupByIDAndSubmissionKeyAfterRestart(t *testing.T) {
	f := newTaskFixture(t)
	run := f.accepted(f.approve()).run()
	f.restart()
	for _, selector := range []map[string]any{{"run_id": run.RunID}, {"idempotency_key": f.key}} {
		result := f.readRun(selector)
		result.readEvidence(run)
		result.persisted(f)
	}
}
func TestRunLookupIsScopedToPrincipalAndProject(t *testing.T) {
	f := newTaskFixture(t)
	run := f.accepted(f.approve()).run()
	for _, scope := range []string{"principal", "project"} {
		t.Run(scope, func(t *testing.T) { f.otherScope(scope, run).denied("NOT_FOUND") })
	}
}
func TestTaskApprovalIsScopedToPrincipal(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.principal("another")
	f.submitAs(approval, "another").denied("APPROVAL_REQUIRED")
	f.taskCounts(1, 0, 0)
}
func TestTaskRevocationBlocksReplayAndRunRead(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	run := f.accepted(approval).run()
	f.revoke()
	f.accepted(approval).denied("PROJECT_DISABLED")
	f.readRun(map[string]any{"run_id": run.RunID}).denied("PROJECT_DISABLED")
}
func TestRunExportDefaultsToDeny(t *testing.T) {
	f := newTaskFixture(t)
	run := f.accepted(f.approve()).run()
	f.denyExports()
	f.readRun(map[string]any{"run_id": run.RunID}).denied("EXPORT_BLOCKED")
}
func TestRunSelectorSchema(t *testing.T) {
	f := newTaskFixture(t)
	for _, selector := range []map[string]any{
		{}, {"run_id": "real_00000000-0000-4000-8000-000000000001"}, {"idempotency_key": "bad"},
		{"run_id": nil}, {"idempotency_key": f.key, "run_id": "sim_" + f.key}, {"idempotency_key": f.key, "cursor": "anything"},
	} {
		f.readRun(selector).denied("INVALID_ARGUMENT")
	}
	f.readRun(map[string]any{"idempotency_key": f.key}).denied("NOT_FOUND")
}
func (f *taskFixture) otherScope(scope string, run RunData) taskAssertion {
	config := f.config.otherScope(scope)
	raw := config.runRequest(f.t, run)
	response := f.scopedDispatch(config, raw)
	return taskAssertion{t: f.t, response: response}
}
func (f *taskFixture) scopedDispatch(config Config, raw []byte) Response {
	broker := f.configuredBroker(config)
	return dispatch(f.t, broker, raw, config.identity())
}
func (f *taskFixture) configuredBroker(config Config) *Broker {
	return testBroker(f.t, f.journal, config)
}
func (c Config) runRequest(t *testing.T, run RunData) []byte {
	return fixtureRequest(t, map[string]any{"operation": "read_run", "project_id": c.project(), "arguments": map[string]any{"run_id": run.RunID}})
}

func (c Config) otherScope(scope string) Config {
	if scope == "principal" {
		c.Grant.PrincipalID = "another"
	}
	if scope == "project" {
		c.Grant.ProjectID = "other-project"
	}
	return c
}
func (c Config) project() string { return c.Grant.ProjectID }
func (c Config) identity() *Identity {
	return &Identity{PrincipalID: c.Grant.PrincipalID, Environment: c.Grant.Environment}
}
func (f *taskFixture) submitAs(approval Approval, principal string) taskAssertion {
	return f.result(f.submission(approval), &Identity{PrincipalID: principal, Environment: "personal"})
}
