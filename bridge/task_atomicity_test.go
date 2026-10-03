package bridge

import (
	"context"
	"errors"
	"testing"
)

func TestTaskAcceptanceRollsBackEveryMutationBoundary(t *testing.T) {
	for _, query := range mutationFailureQueries() {
		t.Run(query, func(t *testing.T) {
			f := newTaskFixture(t)
			approval := f.approve()
			f.fail(query)
			f.failedSubmit(approval)
			f.taskCounts(1, 0, 0)
			f.auditReceipts(1)
		})
	}
}
func requireNoTaskResponse(t *testing.T, response Response, err error) {
	t.Helper()
	if !errors.Is(err, ErrAuditUnavailable) || !response.emptyTaskResponse() {
		t.Fatal("audit failure exposed acceptance", response, err)
	}
}
func TestTaskFailureCanRetrySameApproval(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.failReceipt()
	f.failedSubmit(approval)
	f.repairReceipt()
	f.accepted(approval).run()
	f.taskCounts(1, 1, 1)
}
func TestApprovalAndReceiptAreAtomic(t *testing.T) {
	f := newTaskFixture(t)
	f.fail("CREATE TRIGGER fail_approval BEFORE INSERT ON approvals BEGIN SELECT RAISE(ABORT, 'fixture'); END")
	f.failedApproval()
	f.taskCounts(0, 0, 0)
	f.auditReceipts(0)
}
func TestTaskReplayRequiresDurableReceipt(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.accepted(approval).run()
	f.failReceipt()
	f.failedSubmit(approval)
	f.taskCounts(1, 1, 1)
}
func TestRunReadRequiresDurableReceipt(t *testing.T) {
	f := newTaskFixture(t)
	run := f.accepted(f.approve()).run()
	f.failReceipt()
	f.failedRead(run)
}
func TestCancelledContextCannotAcceptTask(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.cancelledSubmission(approval)
	f.taskCounts(1, 0, 0)
}
func (f *taskFixture) failedApproval() {
	approval, err := f.issue(f.review())
	if !errors.Is(err, ErrAuditUnavailable) || approval.ApprovalID != "" {
		f.t.Fatal("partial approval returned", err)
	}
}
func (f *taskFixture) cancelledSubmission(approval Approval) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := f.call(ctx, f.submission(approval))
	requireNoTaskResponse(f.t, response, err)
}

func mutationFailureQueries() []string {
	return []string{
		"CREATE TRIGGER fail_consume BEFORE UPDATE ON approvals BEGIN SELECT RAISE(ABORT, 'fixture'); END",
		"CREATE TRIGGER fail_run BEFORE INSERT ON task_runs BEGIN SELECT RAISE(ABORT, 'fixture'); END",
		"CREATE TRIGGER fail_receipt BEFORE UPDATE ON receipts WHEN json_extract(NEW.payload, '$.effect') = 'task_accepted' BEGIN SELECT RAISE(ABORT, 'fixture'); END",
	}
}
