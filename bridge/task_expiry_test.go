package bridge

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestGrantExpiryDuringSubmissionWait(t *testing.T) {
	f := newTaskFixture(t)
	approval := f.approve()
	f.expireDuringRequest(2)
	f.accepted(approval).denied("DENIED")
	f.taskCounts(1, 0, 0)
}
func TestGrantExpiryDuringApprovalWait(t *testing.T) {
	f := newTaskFixture(t)
	review := f.review()
	f.expireDuringRequest(3)
	f.rejectedReview(review, "DENIED")
	f.taskCounts(0, 0, 0)
}
func TestGrantExpiryDuringRunReadWait(t *testing.T) {
	f := newTaskFixture(t)
	run := f.accepted(f.approve()).run()
	f.expireDuringRequest(2)
	f.readRun(map[string]any{"run_id": run.RunID}).denied("DENIED")
}
func (f *taskFixture) expireDuringRequest(initialReads int64) {
	f.config.Grant.ExpiresAt = fixtureTime().Add(time.Minute)
	f.config.Clock = expiringClock(initialReads)
	f.rebuild()
}
func expiringClock(initialReads int64) func() time.Time {
	var reads atomic.Int64
	return func() time.Time {
		if reads.Add(1) <= initialReads {
			return fixtureTime()
		}
		return fixtureTime().Add(2 * time.Minute)
	}
}
