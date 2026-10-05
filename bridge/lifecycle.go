package bridge

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

var lifecycleStates = permissions{"accepted": true, "running": true, "completed": true, "failed": true, "uncertain": true}

type lifecycle struct {
	run             RunData
	scope           taskScope
	phase           string
	adapterSequence int64
	eventSequence   int64
	leaseOwner      string
	leaseUntil      string
	token           string
	frozen          string
}

func decodeStatus(payload string) (RunData, error) {
	var run RunData
	if err := json.Unmarshal([]byte(payload), &run); err != nil {
		return run, err
	}
	if !run.statusValid() {
		return RunData{}, errors.New("invalid run lifecycle")
	}
	return run, nil
}
func (run RunData) statusValid() bool {
	return run.Simulated && run.Summary == Label && validRunID(run.RunID) && run.StateVersion > 0 &&
		lifecycleStates[run.State] && revisionPattern.MatchString(run.EnvelopeDigest)
}
func lifecycleFromRow(row store.RunLifecycle) (lifecycle, error) {
	run, err := decodeStatus(row.Payload)
	return lifecycle{run: run, scope: taskScope{principal: row.PrincipalID, environment: row.Environment, project: row.ProjectID},
		phase: row.Phase, adapterSequence: row.AdapterSequence, eventSequence: row.EventSequence, leaseOwner: row.LeaseOwner, leaseUntil: row.LeaseUntil}, err
}
func candidateFromRow(row store.WorkerCandidateRow) (lifecycle, error) {
	run, err := decodeStatus(row.Payload)
	return lifecycle{run: run, scope: taskScope{principal: row.PrincipalID, environment: row.Environment, project: row.ProjectID},
		phase: row.Phase, adapterSequence: row.AdapterSequence, eventSequence: row.EventSequence, leaseOwner: row.LeaseOwner,
		leaseUntil: row.LeaseUntil, token: row.DispatchToken, frozen: row.FrozenReview}, err
}
func (life lifecycle) task() (FrozenTask, error) {
	var review ApprovalReview
	if err := json.Unmarshal([]byte(life.frozen), &review); err != nil {
		return FrozenTask{}, err
	}
	task, err := review.task()
	if err != nil || task.EnvelopeDigest != life.run.EnvelopeDigest {
		return FrozenTask{}, errors.New("run envelope mismatch")
	}
	return task, nil
}
func (life lifecycle) leased(now time.Time) (bool, error) {
	if life.leaseOwner == "" {
		return false, nil
	}
	until, err := time.Parse(time.RFC3339Nano, life.leaseUntil)
	return now.Before(until), err
}
func (life *lifecycle) claim(owner string, now time.Time) {
	life.phase, life.leaseOwner, life.leaseUntil = "active", owner, now.Add(30*time.Second).Format(time.RFC3339Nano)
}
func (life *lifecycle) release() {
	life.leaseOwner, life.leaseUntil = "", ""
	if life.run.State == "completed" || life.run.State == "failed" {
		life.phase = "terminal"
	}
}
func (life *lifecycle) confirm(state, at string) {
	life.run.State, life.run.LastConfirmedState, life.run.LastVerifiedAt = state, state, at
	life.run.StateVersion++
	life.run.AdapterHealth = "available"
	life.run.Phase = ""
	life.run.TerminalReason = ""
}
func (life *lifecycle) uncertain(code string) {
	if life.run.LastConfirmedState == "" {
		life.run.LastConfirmedState = life.run.State
	}
	life.run.State = "uncertain"
	life.run.StateVersion++
	life.run.AdapterHealth = "unavailable"
	life.run.TerminalReason = code
}
func (life *lifecycle) fail(code, at string) {
	life.confirm("failed", at)
	life.run.TerminalReason = code
}
func (life *lifecycle) event(kind, summary, at string, adapterSequence int64) RunEvent {
	life.eventSequence++
	return RunEvent{Sequence: life.eventSequence, AdapterSequence: adapterSequence, At: at, Kind: kind,
		State: life.run.State, Summary: Label + " " + summary, Simulated: true}
}
func (life lifecycle) parameters(before lifecycle) (store.UpdateLifecycleParams, error) {
	raw, err := json.Marshal(life.run)
	return store.UpdateLifecycleParams{Phase: life.phase, State: life.run.State, StateVersion: life.run.StateVersion, Payload: string(raw),
		AdapterSequence: life.adapterSequence, EventSequence: life.eventSequence, LeaseOwner: life.leaseOwner, LeaseUntil: life.leaseUntil,
		RunID: life.run.RunID, StateVersion_2: before.run.StateVersion, LeaseOwner_2: before.leaseOwner}, err
}
func (t *taskTransaction) updateLifecycle(before, after lifecycle) error {
	params, err := after.parameters(before)
	if err != nil {
		return err
	}
	rows, err := t.queries.UpdateLifecycle(t.ctx, params)
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("lifecycle changed")
	}
	return nil
}
func (t *taskTransaction) appendEvent(id string, event RunEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return t.queries.InsertRunEvent(t.ctx, store.InsertRunEventParams{RunID: id, Sequence: event.Sequence, Payload: string(raw)})
}
func (t *taskTransaction) lifecycle(id string) (lifecycle, error) {
	row, err := t.queries.GetLifecycle(t.ctx, id)
	if err != nil {
		return lifecycle{}, err
	}
	return lifecycleFromRow(row)
}

func (t *taskTransaction) insertResult(id, payload string) error {
	return t.queries.InsertRunResult(t.ctx, store.InsertRunResultParams{RunID: id, Payload: payload})
}
