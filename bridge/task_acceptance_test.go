package bridge

import (
	"testing"
)

func TestTaskAcceptanceFreezesHostReview(t *testing.T) {
	f := newTaskFixture(t)
	review := f.review()
	f.assertFrozenReview(review)
	approval := f.approve()
	f.accepted(approval).acceptedEvidence(approval, review)
	f.taskCounts(1, 1, 1)
}
func TestTaskReplayAfterExpiryAndRestart(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	original := f.accepted(approval).run()
	f.afterApprovalExpiry()
	f.restart()
	replay := f.accepted(approval)
	replay.sameRun(original)
	replay.replayed()
	f.taskCounts(1, 1, 1)
}
func TestTaskReplayCanonicalizesTransportJSON(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	original := f.accepted(approval).run()
	replay := f.formattedReplay(approval)
	replay.sameRun(original)
	replay.replayed()
	f.taskCounts(1, 1, 1)
}
func TestTaskIdempotencyConflict(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.accepted(approval).run()
	f.prompt("changed prompt")
	f.accepted(approval).denied("IDEMPOTENCY_CONFLICT")
	f.taskCounts(1, 1, 1)
}
func TestTaskConsumedApprovalCannotCreateAnotherRun(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.accepted(approval).run()
	f.useKey("00000000-0000-4000-8000-000000000003")
	f.accepted(approval).denied("APPROVAL_STALE")
	f.taskCounts(1, 1, 1)
}
func TestTaskMissingApproval(t *testing.T) {
	f := newTaskFixture(t)
	f.missingApproval().denied("APPROVAL_REQUIRED")
	f.taskCounts(0, 0, 0)
}
func TestTaskApprovalExpiryBoundary(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.expireApproval()
	f.accepted(approval).denied("APPROVAL_STALE")
	f.taskCounts(1, 0, 0)
}
func TestTaskChangedEnvelopeCannotUseApproval(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.criteria("different approved result")
	f.accepted(approval).denied("APPROVAL_STALE")
	f.taskCounts(1, 0, 0)
}
func TestTaskStaleHostAuthority(t *testing.T) {
	for _, name := range []string{"spec", "workspace", "profile", "export", "policy"} {
		t.Run(name, func(t *testing.T) {
			f := newTaskFixture(t)
			approval := f.approve()
			code := f.hostChange(name)
			f.accepted(approval).denied(code)
			f.taskCounts(1, 0, 0)
		})
	}
}
func TestAcceptedReplaySurvivesHostRevisionChange(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	original := f.accepted(approval).run()
	f.hostChange("spec")
	f.hostChange("workspace")
	f.accepted(approval).sameRun(original)
}
func TestTaskQueueCapacityPreservesApproval(t *testing.T) {
	f := newTaskFixture(t)
	f.fillQueue()
	approval := f.approve()
	f.useKey("00000000-0000-4000-8000-000000000009")
	f.accepted(approval).denied("LIMIT_EXCEEDED")
	f.taskCounts(6, 5, 5)
}
