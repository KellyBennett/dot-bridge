package bridge

import (
	"database/sql"
	"time"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

func (t *taskTransaction) candidate() (lifecycle, error) {
	row, err := t.scopedCandidate()
	if err == sql.ErrNoRows {
		return lifecycle{}, nil
	}
	if err != nil {
		return lifecycle{}, err
	}
	return candidateFromRow(row)
}
func (scope taskScope) candidateParameters() store.WorkerCandidateParams {
	return store.WorkerCandidateParams{PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project}
}
func (t *receiptTransaction) queryCandidate(params store.WorkerCandidateParams) (store.WorkerCandidateRow, error) {
	return t.queries.WorkerCandidate(t.ctx, params)
}
func (t *taskTransaction) noWork(effect string, run *RunData) (workerClaim, Response, error) {
	response := taskResponse(t.receipt, "")
	response.Receipt.Decision, response.Receipt.Effect, response.Receipt.Outcome = "allowed", effect, "idle"
	if run != nil {
		response = t.receipt.runResponse(*run, "", effect)
	}
	committed, err := t.finish(response)
	return workerClaim{}, committed, err
}
func (t *taskTransaction) commitClaim(before lifecycle, claim workerClaim) (workerClaim, Response, error) {
	if claim.fresh {
		if err := t.prepareIntent(&claim.life); err != nil {
			return workerClaim{}, Response{}, ErrAuditUnavailable
		}
	}
	if err := t.updateLifecycle(before, claim.life); err != nil {
		return workerClaim{}, Response{}, ErrAuditUnavailable
	}
	response := t.receipt.runResponse(claim.life.run, "", "dispatch_claimed")
	committed, err := t.finish(response)
	return claim, committed, err
}
func (t *taskTransaction) prepareIntent(life *lifecycle) error {
	event := life.prepareIntent(t.receipt.RecordedAt)
	return t.appendEvent(life.run.RunID, event)
}
func (claim workerClaim) inspectInput(now time.Time) InspectInput {
	return InspectInput{Token: claim.life.token, AfterSequence: claim.life.adapterSequence, Now: now.Format(time.RFC3339Nano)}
}

func (life *lifecycle) prepareIntent(at string) RunEvent {
	life.run.StateVersion++
	life.run.AdapterHealth, life.run.Phase = "preparing", "dispatch_prepared"
	return life.event("dispatch_prepared", "durable dispatch intent", at, 0)
}

func (t *taskTransaction) scopedCandidate() (store.WorkerCandidateRow, error) {
	return t.queryCandidate(t.receipt.scope().candidateParameters())
}

func (life lifecycle) available(now time.Time) (bool, error) {
	if life.run.RunID == "" {
		return false, nil
	}
	leased, err := life.leased(now)
	return !leased, err
}
func (t *taskTransaction) unavailableCandidate(life lifecycle) (workerClaim, Response, error) {
	if life.run.RunID == "" {
		return t.noWork("worker_idle", nil)
	}
	return t.noWork("worker_busy", &life.run)
}
