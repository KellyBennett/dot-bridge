package bridge

import (
	"errors"
	"time"
)

const stubProgressEvents = 120

func (record stubRecord) inspect(input InspectInput) (AdapterObservation, error) {
	if record.scenario == "loss_after_start" {
		return AdapterObservation{}, errors.New("fixture observation unavailable")
	}
	start, now, valid := input.scriptClock(record.startedAt)
	if !valid {
		return AdapterObservation{}, errors.New("invalid fixture clock")
	}
	observation := record.script(start, now, input.AfterSequence)
	record.injectMalformed(&observation)
	return observation, nil
}
func (record stubRecord) script(start, now time.Time, after int64) AdapterObservation {
	state := record.scriptState(start, now)
	observation := AdapterObservation{State: state, Events: []AdapterEvent{}, LastVerifiedAt: now.Format(time.RFC3339Nano)}
	for sequence := int64(1); sequence <= stubProgressEvents+2; sequence++ {
		event := record.scriptEvent(start, sequence)
		if sequence > after && !eventTime(event).After(now) {
			observation.Events = append(observation.Events, event)
		}
	}
	return observation
}
func (record stubRecord) scriptState(start, now time.Time) string {
	if now.Before(start.Add(2 * time.Second)) {
		return "running"
	}
	if record.scenario == "verification_failure" {
		return "failed"
	}
	return "completed"
}
func (record stubRecord) scriptEvent(start time.Time, sequence int64) AdapterEvent {
	event := AdapterEvent{Sequence: sequence, Kind: "progress", State: "running", Summary: Label + " scripted progress", Simulated: true}
	at := start.Add(time.Duration(sequence-1) * 10 * time.Millisecond)
	if sequence == 1 {
		event.Kind = "started"
		event.Summary = Label + " fixture started"
	}
	if sequence == stubProgressEvents+2 {
		at = start.Add(2 * time.Second)
		event = record.terminalEvent(sequence)
	}
	event.At = at.Format(time.RFC3339Nano)
	return event
}
func (record stubRecord) terminalEvent(sequence int64) AdapterEvent {
	state, outcome := record.terminalOutcome()
	return AdapterEvent{Sequence: sequence, Kind: "terminated", State: state, Summary: Label + " scripted simulated verification " + outcome, Simulated: true}
}
func (record stubRecord) terminalOutcome() (string, string) {
	if record.scenario == "verification_failure" {
		return "failed", "failed"
	}
	return "completed", "passed"
}
func (record stubRecord) injectMalformed(observation *AdapterObservation) {
	if record.scenario == "malformed_events" && len(observation.Events) > 0 {
		observation.Events[0].Simulated = false
	}
}
func eventTime(event AdapterEvent) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, event.At)
	return at
}
func (record stubRecord) result() (AdapterResult, error) {
	task, err := record.task()
	if err != nil {
		return AdapterResult{}, err
	}
	artifact := stubArtifact()
	manifest, err := record.manifest(task, artifact)
	return AdapterResult{Manifest: manifest, Artifacts: []ResultArtifact{artifact}}, err
}
func stubArtifact() ResultArtifact {
	text := Label + "\n--- a/SIMULATED_demo.txt\n+++ b/SIMULATED_demo.txt\n@@ -1 +1 @@\n-fixture before\n+fixture after\n"
	return ResultArtifact{ID: "SIMULATED_patch", Filename: "SIMULATED_PATCH.diff", Summary: Label + " synthetic patch",
		Text: text, Digest: byteDigest([]byte(text)), Simulated: true}
}
func (record stubRecord) manifest(task FrozenTask, artifact ResultArtifact) (ResultManifest, error) {
	start, err := time.Parse(time.RFC3339Nano, record.startedAt)
	if err != nil {
		return ResultManifest{}, err
	}
	manifest := task.manifest(record.profile, artifact)
	manifest.scriptTimes(start)
	manifest.Verification, manifest.Unresolved = record.verification()
	return manifest, nil
}
func (task FrozenTask) manifest(profile string, artifact ResultArtifact) ResultManifest {
	final := task.Envelope.WorkspaceSnapshot
	final.DirtyManifestDigest = byteDigest([]byte("scripted fixture after"))
	return ResultManifest{Summary: Label + " result manifest", EnvelopeDigest: task.EnvelopeDigest, PromptDigest: task.Envelope.PromptDigest,
		AdapterVersion: "stub-v1", ProfileRevision: profile, WorkspaceBaseline: task.Envelope.WorkspaceSnapshot, WorkspaceFinal: final,
		ChangedFiles: []string{"SIMULATED_demo.txt"}, DiffDigest: artifact.Digest, PartialEffects: []string{"scripted fixture edit only"},
		TerminationEvidence: "scripted fixture termination; no process launched", Artifacts: []ArtifactMetadata{artifact.metadata()}, Simulated: true}
}
func (record stubRecord) verification() ([]VerificationEvidence, []string) {
	_, outcome := record.terminalOutcome()
	evidence := scriptedVerification(outcome)
	unresolved := []string{}
	if outcome == "failed" {
		unresolved = append(unresolved, "scripted acceptance check failed")
	}
	return []VerificationEvidence{evidence}, unresolved
}

func (input InspectInput) clockAfter(start time.Time) (time.Time, bool) {
	now, err := time.Parse(time.RFC3339Nano, input.Now)
	return now, err == nil && !now.Before(start)
}

func (manifest *ResultManifest) scriptTimes(start time.Time) {
	manifest.StartedAt, manifest.EndedAt = start.Format(time.RFC3339Nano), start.Add(2*time.Second).Format(time.RFC3339Nano)
}

func (input InspectInput) scriptClock(startedAt string) (time.Time, time.Time, bool) {
	start, err := time.Parse(time.RFC3339Nano, startedAt)
	now, valid := input.clockAfter(start)
	return start, now, err == nil && valid
}

func scriptedVerification(outcome string) VerificationEvidence {
	output := Label + " scripted simulated verification " + outcome
	return VerificationEvidence{Summary: output, Outcome: outcome, Command: "SIMULATED_fixture_check", Output: output, OutputDigest: byteDigest([]byte(output)), Simulated: true}
}
