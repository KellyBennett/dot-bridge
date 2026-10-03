package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type taskFixture struct {
	*bridgeFixture
	envelope TaskEnvelope
	key      string
}

func newTaskFixture(t *testing.T) *taskFixture {
	f := &taskFixture{bridgeFixture: newFixture(t), key: "00000000-0000-4000-8000-000000000001"}
	f.config.taskFixtures()
	f.rebuild()
	f.envelope = f.config.fixtureEnvelope()
	return f
}

func (c *Config) taskFixtures() {
	c.Grant.taskFixtures()
	c.Tasks = fixtureTaskConfig()
}
func (g *Grant) taskFixtures() {
	g.Operations = []string{"read_draft", "submit_task", "read_run"}
	g.ExpiresAt = fixtureTime().Add(24 * time.Hour)
}
func fixtureTaskConfig() *TaskConfig {
	return &TaskConfig{WorkspaceID: "synthetic-workspace",
		Snapshot:        WorkspaceSnapshot{BaseCommit: strings.Repeat("a", 40), DirtyManifestDigest: byteDigest([]byte("clean"))},
		ProfileRevision: byteDigest([]byte("stub-profile")), ExportPolicyRevision: byteDigest([]byte("synthetic-export"))}
}
func (c Config) fixtureEnvelope() TaskEnvelope {
	e := TaskEnvelope{CanonicalVersion: "1", TaskID: "00000000-0000-4000-8000-000000000002",
		ProjectID: c.Grant.ProjectID, Environment: "personal", Prompt: "Synthetic task\nExact bytes. ",
		Specs:          []SpecReference{{DocumentID: "design", Revision: byteDigest([]byte(c.Documents["design"]))}},
		AgentProfileID: "stub-v1", AllowedEffects: []string{"edit_workspace", "run_approved_verification"},
		PolicyRevision: c.Grant.PolicyRevision, AcceptanceCriteria: []string{"Review the scripted synthetic diff"}, Simulated: true}
	e.bindWorkspace(*c.Tasks)
	e.PromptDigest = byteDigest([]byte(e.Prompt))
	return e
}
func (e *TaskEnvelope) bindWorkspace(c TaskConfig) {
	e.WorkspaceID, e.WorkspaceSnapshot = c.WorkspaceID, c.Snapshot
	e.ProfileRevision, e.ExportPolicyRevision = c.ProfileRevision, c.ExportPolicyRevision
}

func testJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (f *taskFixture) review() ApprovalReview {
	review, err := f.broker.ReviewTask(testJSON(f.t, f.envelope))
	if err != nil {
		f.t.Fatal(err)
	}
	return review
}

func (f *taskFixture) approve() Approval {
	approval, err := f.issue(f.review())
	if err != nil {
		f.t.Fatal(err)
	}
	f.persisted(approval.Receipt)
	return approval
}

func (f *taskFixture) request(operation string, arguments any) []byte {
	return fixtureRequest(f.t, map[string]any{"operation": operation, "arguments": arguments})
}

func (f *taskFixture) submission(approval Approval) []byte {
	return f.request("submit_task", map[string]any{"envelope": f.envelope, "approval_id": approval.ApprovalID, "idempotency_key": f.key})
}

func (f *taskFixture) submit(approval Approval) Response {
	return f.invoke(f.submission(approval), fixtureIdentity()).response
}

func requireAccepted(t *testing.T, response Response) RunData {
	t.Helper()
	if !response.validAcceptance() || !response.Run.validAcceptance() {
		t.Fatalf("invalid acceptance: %+v", response)
	}
	return *response.Run
}

func (f *taskFixture) clock(now time.Time) {
	f.config.Clock = func() time.Time { return now }
	f.rebuild()
}

func (f *taskFixture) taskCounts(approvals, runs, consumed int) {
	f.t.Helper()
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM approvals", approvals}, {"SELECT COUNT(*) FROM task_runs", runs},
		{"SELECT COUNT(*) FROM approvals WHERE consumed_run_id IS NOT NULL", consumed},
	} {
		var got int
		if err := f.journal.db.QueryRow(check.query).Scan(&got); err != nil || got != check.want {
			f.t.Fatalf("task storage check: got %d, want %d: %v", got, check.want, err)
		}
	}
}

// taskAssertion owns acceptance checks; host fixtures own policy and storage setup.
type taskAssertion struct {
	t        *testing.T
	response Response
}

func (a taskAssertion) run() RunData { return requireAccepted(a.t, a.response) }
func (a taskAssertion) denied(code string) {
	responseAssertion{t: a.t, response: a.response}.denied(code)
}
func (a taskAssertion) sameRun(expected RunData) {
	a.t.Helper()
	if a.run() != expected {
		a.t.Fatal("run metadata differs")
	}
}
func (a taskAssertion) replayed() {
	a.t.Helper()
	if !a.response.Replayed || a.response.Receipt.Effect != "task_replayed" {
		a.t.Fatal("expected replay evidence")
	}
}
func (f *taskFixture) accepted(approval Approval) taskAssertion {
	return taskAssertion{t: f.t, response: f.submit(approval)}
}
func (f *taskFixture) result(raw []byte, identity *Identity) taskAssertion {
	return taskAssertion{t: f.t, response: f.invoke(raw, identity).response}
}
func (f *taskFixture) readRun(selector map[string]any) taskAssertion {
	return f.result(f.request("read_run", selector), fixtureIdentity())
}

func (r Response) validAcceptance() bool {
	return r.Run != nil && r.Data == nil && r.Receipt.Decision == "allowed" &&
		r.Simulated && r.Receipt.Simulated && r.Summary == Label
}
func (run RunData) validAcceptance() bool {
	return run.valid() && !run.ResultAvailable && validRunID(run.RunID)
}
func (r Response) emptyTaskResponse() bool {
	return r.Run == nil && r.Data == nil && r.Receipt.ReceiptID == ""
}

func (f *taskFixture) hostChange(kind string) string {
	code := f.config.changeTaskAuthority(kind)
	f.rebuild()
	return code
}
func (c *Config) changeTaskAuthority(kind string) string {
	if kind == "spec" {
		c.Documents["design"] += " changed"
		return "APPROVAL_STALE"
	}
	if kind == "policy" {
		c.Grant.PolicyRevision = byteDigest([]byte("new policy"))
		return "APPROVAL_STALE"
	}
	return c.Tasks.changeAuthority(kind)
}
func (c *TaskConfig) changeAuthority(kind string) string {
	switch kind {
	case "workspace":
		c.Snapshot.BaseCommit = strings.Repeat("b", 40)
		return "WORKSPACE_CHANGED"
	case "profile":
		c.ProfileRevision = byteDigest([]byte("new profile"))
	case "export":
		c.ExportPolicyRevision = byteDigest([]byte("new export"))
	}
	return "APPROVAL_STALE"
}

func (f *taskFixture) prompt(text string) {
	f.envelope.Prompt = text
	f.envelope.PromptDigest = byteDigest([]byte(text))
}
func (f *taskFixture) criteria(text string)       { f.envelope.AcceptanceCriteria = []string{text} }
func (f *taskFixture) principal(name string)      { f.config.Grant.PrincipalID = name; f.rebuild() }
func (f *taskFixture) revoke()                    { f.config.Grant.Enabled = false; f.rebuild() }
func (f *taskFixture) denyExports()               { f.config.ExportCheck = nil; f.rebuild() }
func (f *taskFixture) operations(names ...string) { f.config.Grant.Operations = names; f.rebuild() }
func (f *taskFixture) useKey(key string)          { f.key = key }
func (f *taskFixture) fail(query string)          { f.probe().exec(query) }
func (f *taskFixture) failedSubmit(approval Approval) {
	response, err := f.call(context.Background(), f.submission(approval))
	requireNoTaskResponse(f.t, response, err)
}
func (f *taskFixture) auditReceipts(expected int) { f.probe().count(expected) }
func (f *taskFixture) persisted(response Receipt) { f.probe().sameReceipt(response) }
func (f *taskFixture) failedRead(run RunData) {
	response, err := f.call(context.Background(), f.request("read_run", map[string]any{"run_id": run.RunID}))
	requireNoTaskResponse(f.t, response, err)
}
func (f *taskFixture) failReceipt()   { f.probe().failUpdates() }
func (f *taskFixture) repairReceipt() { f.probe().allowUpdates() }

func (r Receipt) acceptanceEvidence(t *testing.T, approval Approval) {
	if r.ApprovalID != approval.ApprovalID || r.Effect != "task_accepted" {
		t.Fatal("acceptance receipt differs")
	}
}

func (f *taskFixture) call(ctx context.Context, raw []byte) (Response, error) {
	return f.broker.Dispatch(ctx, raw, fixtureIdentity())
}
func (f *taskFixture) issue(review ApprovalReview) (Approval, error) {
	return f.broker.IssueApproval(context.Background(), review)
}
func (f *taskFixture) expireApproval()           { f.clock(fixtureTime().Add(30 * time.Minute)) }
func (f *taskFixture) afterApprovalExpiry()      { f.clock(fixtureTime().Add(time.Hour)) }
func (a taskAssertion) persisted(f *taskFixture) { f.persisted(a.response.Receipt) }
func (a taskAssertion) exactInput(raw []byte) {
	if a.response.Receipt.InputDigest != byteDigest(raw) {
		a.t.Fatal("audit lost exact request bytes")
	}
}
func (a taskAssertion) receiptEvidence(approval Approval) {
	a.response.Receipt.acceptanceEvidence(a.t, approval)
}
