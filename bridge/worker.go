package bridge

import (
	"context"
	"errors"
	"time"
)

// Worker is host-driven; Tick does not create a listener or background process.
// Existing intents are reconciled by token and are never started again.
type Worker struct {
	service *taskService
	adapter ExecutionAdapter
}

func (b *Broker) NewWorker(adapter ExecutionAdapter) (*Worker, error) {
	if b.tasks == nil || adapter == nil {
		return nil, errors.New("task worker unavailable")
	}
	if !b.tasks.approvedAdapter(adapter) {
		return nil, errors.New("INTEGRATION_NOT_APPROVED")
	}
	return &Worker{service: b.tasks, adapter: adapter}, nil
}
func (w *Worker) Tick(ctx context.Context) (Response, error) {
	claim, response, err := w.service.prepare(ctx)
	if err != nil || !claim.work {
		return response, err
	}
	execution := w.execution(ctx, claim)
	return execution.execute()
}

type workerClaim struct {
	life    lifecycle
	fresh   bool
	work    bool
	receipt Receipt
}

func (s *taskService) prepare(ctx context.Context) (workerClaim, Response, error) {
	tx, err := s.beginWorker(ctx)
	if err != nil {
		return workerClaim{}, Response{}, err
	}
	defer tx.rollback()
	return tx.claim(s.clock())
}
func (s *taskService) workerReceipt() (Receipt, error) {
	receipt, err := s.policy.approvalReceipt(nil, s.clock())
	receipt.Operation = "host_tick"
	return receipt, err
}
func (tx *taskTransaction) claim(now time.Time) (workerClaim, Response, error) {
	life, err := tx.candidate()
	if err != nil {
		return workerClaim{}, Response{}, ErrAuditUnavailable
	}
	available, err := life.available(now)
	if err != nil {
		return workerClaim{}, Response{}, ErrAuditUnavailable
	}
	if !available {
		return tx.unavailableCandidate(life)
	}
	return tx.claimLife(life, now)
}
func (tx *taskTransaction) claimLife(before lifecycle, now time.Time) (workerClaim, Response, error) {
	owner, err := newUUID()
	if err != nil {
		return workerClaim{}, Response{}, ErrAuditUnavailable
	}
	after := before
	after.claim(owner, now)
	claim := workerClaim{life: after, fresh: before.phase == "queued", work: true, receipt: tx.receipt}
	return tx.commitClaim(before, claim)
}
func (w workerExecution) execute() (Response, error) {
	if w.claim.fresh {
		return w.launch()
	}
	return w.reconcile()
}
func (w workerExecution) launch() (Response, error) {
	task, err := w.claim.task()
	if err != nil {
		return w.resolve(unverified("ADAPTER_PROTOCOL_ERROR"))
	}
	if code := w.launchAuthority(task); code != "" {
		return w.resolve(rejected(code))
	}
	code, err := w.session.preflight(task)
	if err != nil || code != "" {
		return w.resolve(rejected("AGENT_UNAVAILABLE"))
	}
	return w.start(task)
}
func (w workerExecution) launchAuthority(task FrozenTask) string {
	if code := w.service.launchAuthority(w.claim.receipt, task); code != "" {
		return code
	}
	return w.claim.startAuthority(w.service.clock())
}
func (s *taskService) launchAuthority(receipt Receipt, task FrozenTask) string {
	if code := s.policy.receiptAuthority(receipt, s.clock()); code != "" {
		return code
	}
	return s.authority(task.Envelope)
}
func (life lifecycle) startAuthority(now time.Time) string {
	deadline, err := time.Parse(time.RFC3339Nano, life.run.StartDeadline)
	if err != nil || !now.Before(deadline) {
		return "APPROVAL_STALE"
	}
	leased, err := life.leased(now)
	if err != nil || !leased {
		return "START_UNCERTAIN"
	}
	return ""
}
func (s adapterSession) preflight(task FrozenTask) (string, error) {
	if !s.adapter.Capabilities().valid(task.Envelope.ProfileRevision) {
		return "INTEGRATION_NOT_APPROVED", nil
	}
	status, err := s.adapter.Preflight(s.ctx, task)
	if status != "eligible" {
		return "AGENT_UNAVAILABLE", err
	}
	return "", err
}
func (w workerExecution) start(task FrozenTask) (Response, error) {
	if code := w.launchAuthority(task); code != "" {
		return w.resolve(rejected(code))
	}
	return w.resolve(w.dispatchTask(task))
}

func (s *taskService) approvedAdapter(adapter ExecutionAdapter) bool {
	return adapter.Capabilities().valid(s.config.ProfileRevision)
}
func (s *taskService) beginWorker(ctx context.Context) (*taskTransaction, error) {
	receipt, err := s.workerReceipt()
	if err != nil {
		return nil, err
	}
	return s.journal.beginTask(ctx, receipt)
}

type workerExecution struct {
	service *taskService
	session adapterSession
	claim   workerClaim
}

func (w workerExecution) dispatchTask(task FrozenTask) workerUpdate {
	return w.claim.dispatchTask(w.service, w.session, task)
}
func (s adapterSession) dispatch(input DispatchInput) workerUpdate {
	start, err := s.adapter.Start(s.ctx, input)
	if err != nil {
		return unverified("START_UNCERTAIN")
	}
	return start.update(input.StartedAt)
}
func (claim workerClaim) task() (FrozenTask, error)           { return claim.life.task() }
func (claim workerClaim) startAuthority(now time.Time) string { return claim.life.startAuthority(now) }
func (claim workerClaim) dispatchInput(task FrozenTask, now time.Time) DispatchInput {
	return DispatchInput{Token: claim.life.token, Task: task, StartedAt: now.Format(time.RFC3339Nano)}
}

func (w *Worker) execution(ctx context.Context, claim workerClaim) workerExecution {
	return workerExecution{service: w.service, session: w.session(ctx), claim: claim}
}

type adapterSession struct {
	adapter ExecutionAdapter
	ctx     context.Context
}

func (w *Worker) session(ctx context.Context) adapterSession {
	return adapterSession{adapter: w.adapter, ctx: ctx}
}
func (s *taskService) dispatchInput(claim workerClaim, task FrozenTask) DispatchInput {
	return claim.dispatchInput(task, s.clock())
}
func (s *taskService) inspectInput(claim workerClaim) InspectInput {
	return claim.inspectInput(s.clock())
}

func (claim workerClaim) dispatchTask(service *taskService, session adapterSession, task FrozenTask) workerUpdate {
	return session.dispatch(service.dispatchInput(claim, task))
}
