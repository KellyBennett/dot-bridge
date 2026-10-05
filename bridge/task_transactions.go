package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

func (j *Journal) issueApproval(ctx context.Context, approved approvedReview) (Approval, error) {
	tx, err := j.beginTask(ctx, approved.receipt)
	if err != nil {
		return Approval{}, err
	}
	defer tx.rollback()
	return tx.commitApproved(approved)
}
func (t *taskTransaction) commitApproved(approved approvedReview) (Approval, error) {
	if !approved.alive() {
		return t.rejectApproval("DENIED")
	}
	if err := t.insertApproval(approved); err != nil {
		return Approval{}, ErrAuditUnavailable
	}
	if err := t.commit(); err != nil {
		return Approval{}, ErrAuditUnavailable
	}
	approved.approval.Receipt = t.receipt
	return approved.approval, nil
}

func (t *taskTransaction) insertApproval(approved approvedReview) error {
	frozen, err := approved.review.frozen()
	if err != nil {
		return err
	}
	return t.queries.InsertApproval(t.ctx, approved.parameters(frozen))
}

func (a approvedReview) parameters(frozen string) store.InsertApprovalParams {
	return store.InsertApprovalParams{ApprovalID: a.approval.ApprovalID,
		PrincipalID: a.receipt.PrincipalID, Environment: a.receipt.Environment, ProjectID: a.receipt.ProjectID,
		EnvelopeDigest: a.approval.EnvelopeDigest, FrozenReview: frozen, ExpiresAt: a.approval.ExpiresAt}
}

type taskTransaction struct {
	*receiptTransaction
	receipt Receipt
}

// The provisional receipt acquires SQLite's writer lock before replay lookup.
// Concurrent processes serialize approval use, run creation and receipt commit.
func (j *Journal) beginTask(ctx context.Context, receipt Receipt) (*taskTransaction, error) {
	tx, err := j.begin(ctx)
	if err != nil {
		return nil, ErrAuditUnavailable
	}
	recorded, err := tx.store(receipt)
	if err != nil {
		tx.rollback()
		return nil, ErrAuditUnavailable
	}
	return &taskTransaction{receiptTransaction: tx, receipt: recorded}, nil
}

func (t *taskTransaction) finish(response Response) (Response, error) {
	response.Receipt.JournalSequence = t.receipt.JournalSequence
	recorded, err := t.finalize(response.Receipt)
	if err != nil {
		return Response{}, ErrAuditUnavailable
	}
	if err = t.commit(); err != nil {
		return Response{}, ErrAuditUnavailable
	}
	response.Receipt = recorded
	return response, nil
}

type submission struct {
	args     submitTaskArgs
	envelope TaskEnvelope
	receipt  Receipt
	digest   string
}

type submissionIdentity struct {
	Envelope       TaskEnvelope `json:"envelope"`
	ApprovalID     string       `json:"approval_id"`
	IdempotencyKey string       `json:"idempotency_key"`
}

func (input *submission) identify() error {
	canonical, err := json.Marshal(submissionIdentity{Envelope: input.envelope,
		ApprovalID: input.args.ApprovalID, IdempotencyKey: input.args.IdempotencyKey})
	input.digest = byteDigest(canonical)
	return err
}

func (input submission) reject(code string) Response { return taskResponse(input.receipt, code) }

func (s *taskService) submitTask(ctx context.Context, input submission) (Response, error) {
	if err := input.identify(); err != nil {
		return Response{}, ErrAuditUnavailable
	}
	tx, err := s.beginSubmission(ctx, input)
	if err != nil {
		return Response{}, err
	}
	defer tx.rollback()
	response, err := s.acceptTask(tx, input)
	if err != nil {
		return Response{}, ErrAuditUnavailable
	}
	return tx.finish(response)
}

func (s *taskService) acceptTask(tx *taskTransaction, input submission) (Response, error) {
	if code := s.submissionAuthority(input); code != "" {
		return input.reject(code), nil
	}
	response, found, err := tx.replay(input)
	if found || err != nil {
		return response, err
	}
	if code := s.authority(input.envelope); code != "" {
		return input.reject(code), nil
	}
	if code, err := tx.newAuthority(input, s.clock()); err != nil || code != "" {
		return input.reject(code), err
	}
	return tx.enqueue(input, s.clock())
}

type taskScope struct{ principal, environment, project string }

func (r Receipt) scope() taskScope {
	return taskScope{principal: r.PrincipalID, environment: r.Environment, project: r.ProjectID}
}

type storedSubmission struct{ payload, digest, approvalID string }

func (t *taskTransaction) submission(scope taskScope, key string) (storedSubmission, error) {
	row, err := t.querySubmission(scope.submissionParameters(key))
	return storedSubmission{payload: row.Payload, digest: row.InputDigest, approvalID: row.ApprovalID}, err
}
func (scope taskScope) submissionParameters(key string) store.FindSubmissionParams {
	return store.FindSubmissionParams{PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project, IdempotencyKey: key}
}
func (t *receiptTransaction) querySubmission(params store.FindSubmissionParams) (store.FindSubmissionRow, error) {
	return t.queries.FindSubmission(t.ctx, params)
}

func (t *taskTransaction) replay(input submission) (Response, bool, error) {
	prior, err := t.submission(input.scope(), input.key())
	if err == sql.ErrNoRows {
		return Response{}, false, nil
	}
	if err != nil {
		return Response{}, false, err
	}
	response, err := prior.replay(input)
	return response, true, err
}

func (prior storedSubmission) replay(input submission) (Response, error) {
	if prior.digest != input.digest {
		return input.reject("IDEMPOTENCY_CONFLICT"), nil
	}
	run, err := decodeRun(prior.payload)
	if err != nil {
		return Response{}, err
	}
	response := input.receipt.runResponse(run, prior.approvalID, "task_replayed")
	response.Replayed = true
	return response, nil
}

func (r Receipt) runResponse(run RunData, approvalID, effect string) Response {
	r.ApprovalID = approvalID
	r.taskResult(run, effect)
	response := taskResponse(r, "")
	response.Run = &run
	return response
}

type storedApproval store.FindApprovalRow

func (t *taskTransaction) approval(scope taskScope, id string) (storedApproval, error) {
	row, err := t.queries.FindApproval(t.ctx, store.FindApprovalParams{
		ApprovalID: id, PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project})
	return storedApproval(row), err
}

func (t *taskTransaction) newAuthority(input submission, now time.Time) (string, error) {
	approval, err := t.approval(input.scope(), input.approvalID())
	if err == sql.ErrNoRows {
		return "APPROVAL_REQUIRED", nil
	}
	if err != nil {
		return "", err
	}
	if code, err := input.approvalAuthority(approval, now); code != "" || err != nil {
		return code, err
	}
	return t.queueAuthority(input.scope())
}

func (approval storedApproval) authority(e TaskEnvelope, now time.Time) (string, error) {
	canonical, err := e.canonical()
	if err != nil {
		return "", err
	}
	expiry, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
	if err != nil {
		return "", err
	}
	if approval.ConsumedRunID.Valid || approval.EnvelopeDigest != byteDigest(canonical) || !now.Before(expiry) {
		return "APPROVAL_STALE", nil
	}
	return "", nil
}

func (t *taskTransaction) queueAuthority(scope taskScope) (string, error) {
	count, err := t.queries.CountPendingRuns(t.ctx, store.CountPendingRunsParams{ProjectID: scope.project, Environment: scope.environment})
	if err != nil {
		return "", err
	}
	if count >= 5 {
		return "LIMIT_EXCEEDED", nil
	}
	return "", nil
}

func (input submission) newRun(now time.Time) (RunData, error) {
	canonical, err := input.envelope.canonical()
	if err != nil {
		return RunData{}, err
	}
	return acceptedRun(byteDigest(canonical), now)
}

func (t *taskTransaction) enqueue(input submission, now time.Time) (Response, error) {
	run, err := input.newRun(now)
	if err != nil {
		return Response{}, err
	}
	if err = t.consumeApproval(input.approvalID(), run.RunID); err != nil {
		return Response{}, err
	}
	if err = t.insertRun(input, run); err != nil {
		return Response{}, err
	}
	return input.response(run), nil
}

func (t *taskTransaction) consumeApproval(id, runID string) error {
	rows, err := t.queries.ConsumeApproval(t.ctx, store.ConsumeApprovalParams{
		ConsumedRunID: sql.NullString{String: runID, Valid: true}, ApprovalID: id})
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("approval consumption failed")
	}
	return nil
}

func (t *taskTransaction) insertRun(input submission, run RunData) error {
	parameters, err := input.runParameters(run)
	if err != nil {
		return err
	}
	if err = t.queries.InsertTaskRun(t.ctx, parameters); err != nil {
		return err
	}
	return t.insertLifecycle(parameters)
}

func (input submission) runParameters(run RunData) (store.InsertTaskRunParams, error) {
	payload, err := json.Marshal(run)
	if err != nil {
		return store.InsertTaskRunParams{}, err
	}
	token, err := newUUID()
	if err != nil {
		return store.InsertTaskRunParams{}, err
	}
	return store.InsertTaskRunParams{RunID: run.RunID, PrincipalID: input.receipt.PrincipalID, Environment: input.receipt.Environment,
		ProjectID: input.receipt.ProjectID, IdempotencyKey: input.args.IdempotencyKey, InputDigest: input.digest,
		ApprovalID: input.args.ApprovalID, DispatchToken: "sim_" + token, Payload: string(payload)}, nil
}

func decodeRun(payload string) (RunData, error) {
	var run RunData
	if err := json.Unmarshal([]byte(payload), &run); err != nil {
		return run, err
	}
	if !run.valid() {
		return RunData{}, errors.New("invalid stored run")
	}
	return run, nil
}

func (run RunData) valid() bool {
	return run.Simulated && run.Summary == Label && run.State == "accepted" && run.StateVersion == 1
}

func (s *taskService) beginSubmission(ctx context.Context, input submission) (*taskTransaction, error) {
	return s.journal.beginTask(ctx, input.receipt)
}
func (input submission) scope() taskScope   { return input.receipt.scope() }
func (input submission) key() string        { return input.args.IdempotencyKey }
func (input submission) approvalID() string { return input.args.ApprovalID }
func (input submission) approvalAuthority(approval storedApproval, now time.Time) (string, error) {
	return approval.authority(input.envelope, now)
}
func (input submission) response(run RunData) Response {
	return input.receipt.runResponse(run, input.approvalID(), "task_accepted")
}

func (s *taskService) submissionAuthority(input submission) string {
	return s.policy.receiptAuthority(input.receipt, s.clock())
}
func (p accessPolicy) receiptAuthority(receipt Receipt, now time.Time) string {
	identity := &Identity{PrincipalID: receipt.PrincipalID, Environment: receipt.Environment}
	return p.authorize(identity, now)
}
func (t *taskTransaction) rejectApproval(code string) (Approval, error) {
	response, err := t.finish(taskResponse(t.receipt, code))
	if err != nil {
		return Approval{}, err
	}
	return Approval{Summary: Label, Receipt: response.Receipt, Simulated: true}, errors.New(code)
}

func (t *taskTransaction) insertLifecycle(run store.InsertTaskRunParams) error {
	return t.insertLifecycleParameters(lifecycleParameters(run))
}

func lifecycleParameters(run store.InsertTaskRunParams) store.InsertLifecycleParams {
	return store.InsertLifecycleParams{RunID: run.RunID, PrincipalID: run.PrincipalID, Environment: run.Environment, ProjectID: run.ProjectID, Payload: run.Payload}
}
func (t *receiptTransaction) insertLifecycleParameters(params store.InsertLifecycleParams) error {
	return t.queries.InsertLifecycle(t.ctx, params)
}
