package bridge

import (
	"context"
	"encoding/json"
	"errors"
)

// StubAdapter writes only its durable fixture ledger. It never runs commands,
// contacts a provider, reads credentials or interprets task text as a scenario.
type StubAdapter struct {
	journal  *Journal
	scenario string
	profile  string
}

var stubScenarios = permissions{"success": true, "verification_failure": true, "lost_ack": true, "unavailable": true,
	"loss_after_start": true, "malformed_events": true, "missing_manifest": true}

func NewStubAdapter(journal *Journal, scenario string) (*StubAdapter, error) {
	if journal == nil || !stubScenarios[scenario] {
		return nil, errors.New("invalid stub fixture")
	}
	return &StubAdapter{journal: journal, scenario: scenario, profile: stubProfile(scenario)}, nil
}
func stubProfile(scenario string) string {
	return byteDigest([]byte("stub-v1\nscript-v1\n" + scenario))
}
func (a *StubAdapter) Capabilities() AdapterCapabilities {
	return AdapterCapabilities{Version: "stub-v1", Simulated: true, ProfileRevision: a.profile, DurableDispatchLookup: true,
		StructuredEvents: true, ApprovalBehavior: "none"}
}
func (a *StubAdapter) Preflight(ctx context.Context, task FrozenTask) (string, error) {
	if err := ctx.Err(); err != nil {
		return "unavailable", err
	}
	if !task.valid() || task.Envelope.ProfileRevision != a.profile || !task.Envelope.stubEffects() {
		return "denied", nil
	}
	if a.scenario == "unavailable" {
		return "unavailable", nil
	}
	return "eligible", nil
}
func (a *StubAdapter) Start(ctx context.Context, input DispatchInput) (StartObservation, error) {
	if code, err := a.Preflight(ctx, input.Task); code != "eligible" || err != nil {
		return StartObservation{Status: "rejected", ErrorCode: "AGENT_UNAVAILABLE"}, err
	}
	if err := a.journal.startStub(ctx, input, a.scenario); err != nil {
		return StartObservation{}, err
	}
	if a.scenario == "lost_ack" {
		return StartObservation{Status: "uncertain", ErrorCode: "START_UNCERTAIN"}, nil
	}
	return StartObservation{Status: "started"}, nil
}
func (a *StubAdapter) Inspect(ctx context.Context, input InspectInput) (AdapterObservation, error) {
	record, err := a.record(ctx, input.Token)
	if err != nil {
		return AdapterObservation{}, err
	}
	return record.inspect(input)
}
func (a *StubAdapter) Collect(ctx context.Context, token string) (AdapterResult, error) {
	record, err := a.record(ctx, token)
	if err != nil {
		return AdapterResult{}, err
	}
	if record.profile != a.profile || record.scenario == "missing_manifest" {
		return AdapterResult{}, errors.New("result unavailable")
	}
	return record.result()
}

type stubRecord struct{ token, digest, profile, startedAt, scenario, frozen string }

func (record stubRecord) task() (FrozenTask, error) {
	var task FrozenTask
	if err := json.Unmarshal([]byte(record.frozen), &task); err != nil {
		return task, err
	}
	if !task.valid() || task.EnvelopeDigest != record.digest || task.Envelope.ProfileRevision != record.profile {
		return task, errors.New("invalid fixture ledger")
	}
	return task, nil
}

func (e TaskEnvelope) stubEffects() bool {
	effects, _ := permissionSet(e.AllowedEffects, func(s string) bool { return s == "edit_workspace" || s == "run_approved_verification" })
	return effects["edit_workspace"] && effects["run_approved_verification"]
}

func (a *StubAdapter) record(ctx context.Context, token string) (stubRecord, error) {
	record, err := a.journal.stubRecord(ctx, token)
	if err == nil && record.profile != a.profile {
		return stubRecord{}, errors.New("profile mismatch")
	}
	return record, err
}
