package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvelopeCanonicalVersionAndPromptBytes(t *testing.T) {
	f := newTaskFixture(t)
	f.canonicalEvidence()
	f.stalePromptRejected()
}
func TestEnvelopeStrictNestedFields(t *testing.T) {
	f := newTaskFixture(t)
	for _, mutation := range invalidEnvelopeFields() {
		f.invalidEnvelopeFields(mutation)
	}
}
func invalidEnvelopeFields() []func(map[string]any) {
	return []func(map[string]any){
		func(m map[string]any) { m["command"] = "shell" }, func(m map[string]any) { delete(m, "simulated") },
		func(m map[string]any) { m["prompt"] = nil }, func(m map[string]any) { m["workspace_snapshot"].(map[string]any)["path"] = "/tmp" },
		func(m map[string]any) { delete(m["workspace_snapshot"].(map[string]any), "base_commit") },
		func(m map[string]any) { m["specs"].([]any)[0].(map[string]any)["url"] = "https://invalid" },
		func(m map[string]any) { m["specs"].([]any)[0] = nil },
	}
}
func TestEnvelopeRejectsUnsupportedSemantics(t *testing.T) {
	f := newTaskFixture(t)
	for _, mutation := range invalidEnvelopeSemantics() {
		f.invalidEnvelopeSemantics(mutation)
	}
}
func invalidEnvelopeSemantics() []func(*TaskEnvelope) {
	return []func(*TaskEnvelope){
		func(e *TaskEnvelope) { e.Simulated = false }, func(e *TaskEnvelope) { e.Environment = "employer" },
		func(e *TaskEnvelope) { e.CanonicalVersion = "2" }, func(e *TaskEnvelope) { e.AgentProfileID = "claude-code-local-v1" },
		func(e *TaskEnvelope) { e.AllowedEffects = []string{"push"} }, func(e *TaskEnvelope) { e.Specs = append(e.Specs, e.Specs[0]) },
		func(e *TaskEnvelope) { e.AcceptanceCriteria = nil },
	}
}
func TestEnvelopeDuplicateJSONKeysRejected(t *testing.T) {
	f := newTaskFixture(t)
	f.rejectDuplicateEnvelopeKey()
}
func TestPromptByteLimitSupportsEscapedAndMultibyteText(t *testing.T) {
	f := newTaskFixture(t)
	f.prompt(strings.Repeat("\x00", MaxPromptBytes))
	f.assertLargeSubmission(f.approve())
	f.prompt(strings.Repeat("é", MaxPromptBytes/2+1))
	f.invalidReview()
}
func TestHostApprovalRejectsChangedReview(t *testing.T) {
	f := newTaskFixture(t)
	f.changedReviewRejected()
	f.taskCounts(0, 0, 0)
}
func TestHostReviewPrincipalChangeRequiresNewReview(t *testing.T) {
	f := newTaskFixture(t)
	review := f.review()
	f.principal("another")
	f.rejectedReview(review, "APPROVAL_STALE")
}
func TestPublicContractCannotIssueApproval(t *testing.T) {
	f := newTaskFixture(t)
	f.invoke(f.request("host_approve_task", f.review()), fixtureIdentity()).denied("INVALID_ARGUMENT")
	f.taskCounts(0, 0, 0)
}
func TestTaskOperationGrantsAreIndependent(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.operations("read_run")
	f.accepted(approval).denied("DENIED")
}
func (f *taskFixture) canonicalEvidence() {
	canonical, err := f.envelope.canonical()
	if err != nil {
		f.t.Fatal(err)
	}
	f.review().canonicalEvidence(f.t, canonical)
}
func (review ApprovalReview) canonicalEvidence(t *testing.T, canonical []byte) {
	if review.EnvelopeDigest != "sha256:27c735232ae1a04e7429c76293e4744b9ff4587a40bdecedd15e9746417c90d3" ||
		review.EnvelopeDigest != byteDigest(canonical) || !strings.Contains(review.CanonicalEnvelope, "Exact bytes. ") ||
		!strings.HasPrefix(review.CanonicalEnvelope, `{"canonical_version":"1","task_id":`) {
		t.Fatal("canonicalization changed approved bytes")
	}
}
func (f *taskFixture) stalePromptRejected() {
	f.envelope.Prompt = strings.TrimSpace(f.envelope.Prompt)
	f.invalidReview()
}
func (f *taskFixture) invalidReview() {
	if _, err := f.broker.ReviewTask(testJSON(f.t, f.envelope)); err == nil {
		f.t.Fatal("invalid envelope accepted")
	}
}
func (f *taskFixture) invalidEnvelopeFields(mutation func(map[string]any)) {
	object := f.envelopeObject()
	mutation(object)
	if _, err := f.broker.ReviewTask(testJSON(f.t, object)); err == nil {
		f.t.Fatal("invalid nested envelope accepted")
	}
}
func (f *taskFixture) envelopeObject() map[string]any {
	var object map[string]any
	if err := json.Unmarshal(testJSON(f.t, f.envelope), &object); err != nil {
		f.t.Fatal(err)
	}
	return object
}
func (f *taskFixture) invalidEnvelopeSemantics(mutation func(*TaskEnvelope)) {
	envelope := f.envelope
	mutation(&envelope)
	if _, err := parseEnvelope(testJSON(f.t, envelope)); err == nil {
		f.t.Fatal("unsupported envelope accepted")
	}
}
func (f *taskFixture) rejectDuplicateEnvelopeKey() {
	raw := strings.Replace(string(testJSON(f.t, f.envelope)), `"canonical_version":"1"`, `"canonical_version":"1","canonical_version":"1"`, 1)
	if _, err := f.broker.ReviewTask([]byte(raw)); err == nil {
		f.t.Fatal("duplicate envelope key accepted")
	}
}
func (f *taskFixture) assertLargeSubmission(approval Approval) {
	if len(f.submission(approval)) <= MaxRequestBytes {
		f.t.Fatal("fixture did not exceed old read limit")
	}
	f.accepted(approval).run()
}
func (f *taskFixture) changedReviewRejected() {
	review := f.review()
	review.FrozenSpecs[0].Text += " changed"
	f.rejectedReview(review, "APPROVAL_STALE")
}
func (f *taskFixture) rejectedReview(review ApprovalReview, code string) {
	approval, err := f.issue(review)
	if err == nil || approval.ApprovalID != "" || approval.Receipt.ErrorCode != code {
		f.t.Fatal("changed review became authority", err)
	}
	f.persisted(approval.Receipt)
}
