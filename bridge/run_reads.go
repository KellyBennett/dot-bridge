package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

func (s *taskService) readRun(ctx context.Context, lookup runLookup) (Response, error) {
	tx, err := lookup.begin(ctx, s.journal)
	if err != nil {
		return Response{}, err
	}
	defer tx.rollback()
	response, err := lookup.read(s, tx)
	if err != nil {
		return Response{}, ErrAuditUnavailable
	}
	return tx.finish(response)
}

func (t *taskTransaction) run(scope taskScope, id string) (storedSubmission, error) {
	row, err := t.queryRun(scope.runParameters(id))
	return storedSubmission{payload: row.Payload, approvalID: row.ApprovalID}, err
}
func (scope taskScope) runParameters(id string) store.FindRunParams {
	return store.FindRunParams{PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project, RunID: id}
}
func (t *receiptTransaction) queryRun(params store.FindRunParams) (store.FindRunRow, error) {
	return t.queries.FindRun(t.ctx, params)
}

func (t *taskTransaction) selectedRun(args readRunArgs) (storedSubmission, error) {
	if args.IdempotencyKey != "" {
		return t.submission(t.receipt.scope(), args.IdempotencyKey)
	}
	return t.run(t.receipt.scope(), args.RunID)
}

func (s *taskService) runSnapshot(tx *taskTransaction, args readRunArgs) (Response, error) {
	response, err := tx.runSnapshot(args, s.clock())
	if err != nil {
		return Response{}, err
	}
	return s.exportRun(response), nil
}
func (t *taskTransaction) runSnapshot(args readRunArgs, now time.Time) (Response, error) {
	prior, found, err := t.optionalRun(args)
	if !found && err == nil {
		return taskResponse(t.receipt, "NOT_FOUND"), nil
	}
	if err != nil {
		return Response{}, err
	}
	response, err := prior.response(t.receipt)
	if err != nil {
		return Response{}, err
	}
	return t.readDetails(args, response, now)
}
func (prior storedSubmission) response(receipt Receipt) (Response, error) {
	run, err := decodeRun(prior.payload)
	if err != nil {
		return Response{}, err
	}
	response := receipt.runResponse(run, prior.approvalID, "run_read")
	err = response.describeRunExport()
	return response, err
}

func (response *Response) describeRunExport() error {
	exported, err := response.runExport()
	response.Receipt.OutputDigest, response.Receipt.ExportedBytes = byteDigest(exported), len(exported)
	return err
}

func (s *taskService) exportRun(response Response) Response {
	if response.Run == nil {
		return response
	}
	exported, _ := response.runExport()
	if code := s.drafts.export(string(exported)); code != "" {
		return taskResponse(response.Receipt.withoutRun(), code)
	}
	return response
}

func (r Receipt) withoutRun() Receipt {
	r.RunID, r.ApprovalID, r.EnvelopeDigest, r.OutputDigest = "", "", "", ""
	r.StateVersion, r.ExportedBytes = 0, 0
	return r
}

func (t *taskTransaction) optionalRun(args readRunArgs) (storedSubmission, bool, error) {
	prior, err := t.selectedRun(args)
	if err == sql.ErrNoRows {
		return prior, false, nil
	}
	return prior, err == nil, err
}

func (lookup runLookup) begin(ctx context.Context, journal *Journal) (*taskTransaction, error) {
	return journal.beginTask(ctx, lookup.receipt)
}
func (lookup runLookup) read(service *taskService, tx *taskTransaction) (Response, error) {
	if code := service.runAuthority(tx); code != "" {
		return taskResponse(tx.receipt, code), nil
	}
	return service.runSnapshot(tx, lookup.args)
}

func (s *taskService) runAuthority(tx *taskTransaction) string {
	return s.policy.receiptAuthority(tx.receipt, s.clock())
}

func (response Response) runExport() ([]byte, error) {
	return json.Marshal(runExport{Run: response.Run, Events: response.Events, Result: response.Result, Artifacts: response.Artifacts,
		NextCursor: response.NextCursor, Truncated: response.Truncated})
}

type runExport struct {
	Run        *RunData         `json:"run"`
	Events     []RunEvent       `json:"events,omitempty"`
	Result     *ResultManifest  `json:"result,omitempty"`
	Artifacts  []ResultArtifact `json:"artifacts,omitempty"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Truncated  bool             `json:"truncated,omitempty"`
}
