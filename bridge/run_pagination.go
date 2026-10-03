package bridge

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

func (t *taskTransaction) eventPage(args readRunArgs, response *Response) (string, error) {
	after, code, err := t.afterCursor(response.runID(), args.EventCursor)
	if code != "" || err != nil {
		return code, err
	}
	events, err := t.eventRows(response.runID(), after)
	if err != nil {
		return "", err
	}
	response.applyEvents(events, args.EventCursor)
	return response.advanceCursor(t)
}
func (response *Response) applyEvents(events []RunEvent, cursor string) {
	response.Events, response.Truncated = boundedEventPage(events)
	response.NextCursor = cursor
}
func (response Response) lastSequence() int64 {
	return response.Events[len(response.Events)-1].Sequence
}
func boundedEventPage(events []RunEvent) ([]RunEvent, bool) {
	if len(events) > 100 {
		return events[:100], true
	}
	return events, false
}
func (t *taskTransaction) afterCursor(id, cursor string) (int64, string, error) {
	if cursor == "" {
		return 0, "", nil
	}
	sequence, err := t.queryCursor(t.receipt.scope().findCursorParameters(id, cursor))
	if err == sql.ErrNoRows {
		return 0, "CURSOR_EXPIRED", nil
	}
	return sequence, "", err
}
func (scope taskScope) findCursorParameters(id, cursor string) store.FindRunCursorParams {
	return store.FindRunCursorParams{RunID: id, CursorID: cursor, PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project}
}
func (t *taskTransaction) eventRows(id string, after int64) ([]RunEvent, error) {
	rows, err := t.queries.ListRunEvents(t.ctx, store.ListRunEventsParams{RunID: id, Sequence: after})
	if err != nil {
		return nil, err
	}
	return decodeEvents(rows, after)
}
func decodeEvents(rows []string, after int64) ([]RunEvent, error) {
	events := []RunEvent{}
	for _, raw := range rows {
		event, err := decodeEvent(raw, after+1)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
		after = event.Sequence
	}
	return events, nil
}
func decodeEvent(raw string, sequence int64) (RunEvent, error) {
	var event RunEvent
	if len(raw) > 2048 {
		return event, errors.New("oversized event")
	}
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return event, err
	}
	if !event.Simulated || event.Sequence != sequence || !labelled(event.Summary) || !safeText(event.Summary) {
		return event, errors.New("invalid event")
	}
	return event, nil
}
func (t *taskTransaction) nextCursor(id string, after int64) (string, error) {
	params := t.receipt.scope().getCursorParameters(id, after)
	existing, err := t.queryNextCursor(params)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	return t.createCursor(params)
}
func (scope taskScope) getCursorParameters(id string, after int64) store.GetRunCursorParams {
	return store.GetRunCursorParams{RunID: id, PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project, AfterSequence: after}
}
func (t *taskTransaction) createCursor(params store.GetRunCursorParams) (string, error) {
	id, err := newUUID()
	if err != nil {
		return "", err
	}
	err = t.queries.InsertRunCursor(t.ctx, cursorParameters("cur_"+id, params))
	return "cur_" + id, err
}
func cursorParameters(id string, params store.GetRunCursorParams) store.InsertRunCursorParams {
	return store.InsertRunCursorParams{CursorID: id, RunID: params.RunID, PrincipalID: params.PrincipalID, Environment: params.Environment,
		ProjectID: params.ProjectID, AfterSequence: params.AfterSequence}
}

func (t *receiptTransaction) queryCursor(params store.FindRunCursorParams) (int64, error) {
	return t.queries.FindRunCursor(t.ctx, params)
}
func (t *receiptTransaction) queryNextCursor(params store.GetRunCursorParams) (string, error) {
	return t.queries.GetRunCursor(t.ctx, params)
}

func (response *Response) advanceCursor(t *taskTransaction) (string, error) {
	if len(response.Events) == 0 {
		return "", nil
	}
	cursor, err := t.nextCursor(response.runID(), response.lastSequence())
	response.NextCursor = cursor
	return "", err
}
