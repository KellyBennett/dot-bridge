package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

func (j *Journal) startStub(ctx context.Context, input DispatchInput, scenario string) error {
	tx, err := j.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.rollback()
	if err = tx.insertStub(input, scenario); err != nil {
		return err
	}
	record, err := tx.stubRecord(input.Token)
	if err != nil || !record.matches(input) {
		return errors.New("dispatch token conflict")
	}
	return tx.commit()
}
func (input DispatchInput) parameters(scenario string) (store.InsertStubDispatchParams, error) {
	if _, err := time.Parse(time.RFC3339Nano, input.StartedAt); err != nil {
		return store.InsertStubDispatchParams{}, err
	}
	raw, err := json.Marshal(input.Task)
	return store.InsertStubDispatchParams{DispatchToken: input.Token, EnvelopeDigest: input.Task.EnvelopeDigest,
		ProfileRevision: input.Task.Envelope.ProfileRevision, StartedAt: input.StartedAt, Scenario: scenario, FrozenTask: string(raw)}, err
}
func (t *receiptTransaction) insertStub(input DispatchInput, scenario string) error {
	params, err := input.parameters(scenario)
	if err != nil {
		return err
	}
	return t.queries.InsertStubDispatch(t.ctx, params)
}
func (record stubRecord) matches(input DispatchInput) bool {
	return record.digest == input.Task.EnvelopeDigest && record.profile == input.Task.Envelope.ProfileRevision
}
func (t *receiptTransaction) stubRecord(token string) (stubRecord, error) {
	row, err := t.queries.FindStubDispatch(t.ctx, token)
	return stubFromRow(row), err
}
func (j *Journal) stubRecord(ctx context.Context, token string) (stubRecord, error) {
	row, err := j.queryStubRecord(ctx, token)
	return stubFromRow(row), err
}
func stubFromRow(row store.StubDispatch) stubRecord {
	return stubRecord{token: row.DispatchToken, digest: row.EnvelopeDigest, profile: row.ProfileRevision,
		startedAt: row.StartedAt, scenario: row.Scenario, frozen: row.FrozenTask}
}

func (j *Journal) queryStubRecord(ctx context.Context, token string) (store.StubDispatch, error) {
	return store.New(j.db).FindStubDispatch(ctx, token)
}
