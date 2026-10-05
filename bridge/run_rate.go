package bridge

import (
	"database/sql"
	"github.com/KellyBennett/dot-bridge/internal/store"
	"time"
)

// Two successful lookup attempts per rolling second, shared across project runs.
func (t *taskTransaction) poll(now time.Time) (string, error) {
	scope := t.receipt.scope()
	previous, err := t.pollWindow(scope)
	if err == sql.ErrNoRows {
		return t.recordPoll(scope, newPollWindow(now))
	}
	if err != nil {
		return "", err
	}
	return t.advancePoll(scope, previous, now)
}

type pollWindow struct {
	first, last string
	count       int64
}

func newPollWindow(now time.Time) pollWindow {
	at := now.Format(time.RFC3339Nano)
	return pollWindow{first: at, last: at, count: 1}
}
func (window pollWindow) next(now time.Time) (pollWindow, string, error) {
	first, last, err := window.times()
	if err != nil {
		return window, "", err
	}
	if now.Before(last) {
		return window, "LIMIT_EXCEEDED", nil
	}
	if !now.Before(last.Add(time.Second)) {
		return newPollWindow(now), "", nil
	}
	if window.count == 2 && now.Before(first.Add(time.Second)) {
		return window, "LIMIT_EXCEEDED", nil
	}
	return pollWindow{first: window.latestFirst(), last: now.Format(time.RFC3339Nano), count: 2}, "", nil
}
func (window pollWindow) latestFirst() string {
	if window.count == 2 {
		return window.last
	}
	return window.first
}
func (scope taskScope) pollParameters() store.GetPollWindowParams {
	return store.GetPollWindowParams{PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project}
}
func (t *taskTransaction) advancePoll(scope taskScope, window pollWindow, now time.Time) (string, error) {
	next, code, err := window.next(now)
	if code != "" || err != nil {
		return code, err
	}
	return t.recordPoll(scope, next)
}
func (t *taskTransaction) recordPoll(scope taskScope, window pollWindow) (string, error) {
	return "", t.setPollWindow(scope.setPollParameters(window))
}
func (scope taskScope) setPollParameters(window pollWindow) store.SetPollWindowParams {
	return store.SetPollWindowParams{PrincipalID: scope.principal, Environment: scope.environment, ProjectID: scope.project, StartedAt: window.first, LastAt: window.last, Polls: window.count}
}
func (t *receiptTransaction) setPollWindow(params store.SetPollWindowParams) error {
	return t.queries.SetPollWindow(t.ctx, params)
}
func (t *receiptTransaction) queryPollWindow(params store.GetPollWindowParams) (store.GetPollWindowRow, error) {
	return t.queries.GetPollWindow(t.ctx, params)
}
func (t *taskTransaction) pollWindow(scope taskScope) (pollWindow, error) {
	row, err := t.queryPollWindow(scope.pollParameters())
	return pollWindow{first: row.StartedAt, last: row.LastAt, count: row.Polls}, err
}

func (window pollWindow) times() (time.Time, time.Time, error) {
	first, err := time.Parse(time.RFC3339Nano, window.first)
	last, lastErr := time.Parse(time.RFC3339Nano, window.last)
	if err != nil {
		return first, last, err
	}
	return first, last, lastErr
}
