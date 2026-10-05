package bridge

import (
	"database/sql"
	"time"
)

type workerUpdate struct {
	state         string
	code          string
	at            string
	events        []AdapterEvent
	result        *AdapterResult
	verified      bool
	lastConfirmed string
}

func rejected(code string) workerUpdate   { return workerUpdate{state: "failed", code: code} }
func unverified(code string) workerUpdate { return workerUpdate{state: "uncertain", code: code} }
func (start StartObservation) update(at string) workerUpdate {
	switch start.Status {
	case "started":
		return workerUpdate{state: "running", at: at, verified: true}
	case "rejected":
		return rejected("AGENT_UNAVAILABLE")
	default:
		return unverified("START_UNCERTAIN")
	}
}
func (w workerExecution) reconcile() (Response, error) {
	return w.collectAndResolve(w.inspected())
}
func (w workerExecution) inspected() workerUpdate {
	return w.claim.inspected(w.service, w.session)
}
func (s adapterSession) inspect(input InspectInput) (AdapterObservation, error) {
	return s.adapter.Inspect(s.ctx, input)
}
func (claim workerClaim) inspectUpdate(input InspectInput, observation AdapterObservation, err error) workerUpdate {
	now, _ := time.Parse(time.RFC3339Nano, input.Now)
	return claim.life.inspectUpdate(observation, now, err)
}
func (life lifecycle) inspectUpdate(observation AdapterObservation, now time.Time, err error) workerUpdate {
	if err == sql.ErrNoRows {
		return unverified("DISPATCH_UNCERTAIN")
	}
	if err != nil {
		return life.unavailable(now)
	}
	update, err := life.observed(observation, now)
	if err != nil {
		return unverified("ADAPTER_PROTOCOL_ERROR")
	}
	return update
}
func (life lifecycle) unavailable(now time.Time) workerUpdate {
	verified, err := time.Parse(time.RFC3339Nano, life.run.LastVerifiedAt)
	if err != nil || !now.Before(verified.Add(30*time.Second)) {
		return unverified("AGENT_UNAVAILABLE")
	}
	return workerUpdate{state: life.run.State, code: "AGENT_UNAVAILABLE"}
}
func (life lifecycle) observed(observation AdapterObservation, now time.Time) (workerUpdate, error) {
	if err := life.validateObservation(observation, now); err != nil {
		return workerUpdate{}, err
	}
	return workerUpdate{state: observation.State, at: observation.LastVerifiedAt, events: observation.Events, verified: true}, nil
}
func (w workerExecution) collectAndResolve(update workerUpdate) (Response, error) {
	if update.terminal() {
		update = w.collected(update)
	}
	return w.resolve(update)
}
func (update workerUpdate) terminal() bool {
	return update.state == "completed" || update.state == "failed"
}
func (w workerExecution) collected(update workerUpdate) workerUpdate {
	result, err := w.session.collect(w.claim.token())
	if err != nil {
		return update.withheld("RESULT_NOT_READY")
	}
	if !w.claim.validResult(result, update) {
		return update.withheld("ADAPTER_PROTOCOL_ERROR")
	}
	update.result = &result
	return update
}
func (update workerUpdate) withheld(code string) workerUpdate {
	update.lastConfirmed = update.state
	update.state, update.code, update.verified = "uncertain", code, false
	return update
}
func (update workerUpdate) apply(life *lifecycle, now string) {
	if update.verified {
		life.confirm(update.state, update.at)
	} else {
		update.applyUnverified(life, now)
	}
	update.applyEvidence(life)
	life.release()
}
func (update workerUpdate) applyUnverified(life *lifecycle, now string) {
	if update.state == "uncertain" {
		life.uncertain(update.code)
		return
	}
	if update.state == "failed" {
		life.fail(update.code, now)
		return
	}
	life.run.StateVersion++
	life.run.AdapterHealth = "unavailable"
}

func (claim workerClaim) validResult(result AdapterResult, update workerUpdate) bool {
	task, err := claim.life.task()
	return err == nil && result.valid(task, update)
}

func (s adapterSession) collect(token string) (AdapterResult, error) {
	return s.adapter.Collect(s.ctx, token)
}
func (claim workerClaim) token() string { return claim.life.token }

func (claim workerClaim) inspected(service *taskService, session adapterSession) workerUpdate {
	input := service.inspectInput(claim)
	observation, err := session.inspect(input)
	return claim.inspectUpdate(input, observation, err)
}

func (update workerUpdate) applyEvidence(life *lifecycle) {
	if update.lastConfirmed != "" {
		life.run.LastConfirmedState = update.lastConfirmed
		life.run.LastVerifiedAt = update.at
	}
	life.run.ResultAvailable = update.result != nil
	if update.state == "failed" && update.code != "" {
		life.run.TerminalReason = update.code
	}
}
