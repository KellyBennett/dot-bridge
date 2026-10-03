package bridge

import (
	"context"
	"errors"
)

var ErrWorkerLeaseLost = errors.New("worker lease lost; reconcile the existing dispatch token")

func (w workerExecution) resolve(update workerUpdate) (Response, error) {
	return w.session.resolve(w.service, w.claim, update)
}
func (s *taskService) resolve(ctx context.Context, claim workerClaim, update workerUpdate) (Response, error) {
	tx, err := s.beginWorker(ctx)
	if err != nil {
		return Response{}, err
	}
	defer tx.rollback()
	return tx.resolveClaim(claim, update)
}
func (t *taskTransaction) resolveClaim(claim workerClaim, update workerUpdate) (Response, error) {
	before, err := claim.current(t)
	if err != nil {
		return Response{}, err
	}
	after := before
	update.apply(&after, t.receipt.RecordedAt)
	if err = t.persistObservation(before, &after, update); err != nil {
		return Response{}, ErrAuditUnavailable
	}
	return t.finish(t.observationResponse(after, update))
}
func (t *taskTransaction) persistObservation(before lifecycle, after *lifecycle, update workerUpdate) error {
	if err := t.persistAdapterEvents(after, update.events); err != nil {
		return err
	}
	if err := t.persistStatusEvent(before, after, update); err != nil {
		return err
	}
	if update.result != nil {
		if err := t.persistResult(after.run.RunID, *update.result); err != nil {
			return err
		}
	}
	return t.updateLifecycle(before, *after)
}
func (t *taskTransaction) persistAdapterEvents(after *lifecycle, events []AdapterEvent) error {
	for _, event := range events {
		after.adapterSequence = event.Sequence
		after.eventSequence++
		recorded := event.runEvent(after.eventSequence)
		if err := t.appendEvent(after.run.RunID, recorded); err != nil {
			return err
		}
	}
	return nil
}
func (event AdapterEvent) runEvent(sequence int64) RunEvent {
	return RunEvent{Sequence: sequence, AdapterSequence: event.Sequence, At: event.At, Kind: event.Kind, State: event.State, Summary: event.Summary, Simulated: true}
}
func (t *taskTransaction) persistStatusEvent(before lifecycle, after *lifecycle, update workerUpdate) error {
	if !after.statusChanged(before) {
		return nil
	}
	event := after.statusEvent(update.verified, t.receipt.RecordedAt)
	return t.appendEvent(after.run.RunID, event)
}
func (life lifecycle) statusChanged(before lifecycle) bool {
	return before.run.State != life.run.State || before.run.AdapterHealth != life.run.AdapterHealth
}
func (life *lifecycle) statusEvent(verified bool, at string) RunEvent {
	summary := "verified state: " + life.run.State
	if !verified {
		summary = "observation: " + life.run.State
	}
	return life.event("state_observed", summary, at, 0)
}
func (claim workerClaim) current(tx *taskTransaction) (lifecycle, error) {
	life, err := tx.lifecycle(claim.life.run.RunID)
	if err != nil {
		return lifecycle{}, ErrAuditUnavailable
	}
	if life.leaseOwner != claim.life.leaseOwner {
		return lifecycle{}, ErrWorkerLeaseLost
	}
	return life, nil
}
func (t *taskTransaction) persistResult(id string, result AdapterResult) error {
	payload, err := result.encode()
	if err != nil {
		return err
	}
	return t.insertResult(id, payload)
}
func (t *taskTransaction) observationResponse(after lifecycle, update workerUpdate) Response {
	response := t.receipt.runResponse(after.run, "", "run_observed")
	response.Receipt.ErrorCode = update.code
	if update.code != "" && after.run.State == "failed" {
		response.Receipt.Decision = "denied"
	}
	return response
}

func (session adapterSession) resolve(service *taskService, claim workerClaim, update workerUpdate) (Response, error) {
	return service.resolve(session.ctx, claim, update)
}
