package bridge

import (
	"encoding/json"
	"strings"
	"time"
)

func (result AdapterResult) valid(task FrozenTask, update workerUpdate) bool {
	raw, err := json.Marshal(result)
	return err == nil && len(raw) <= 192*1024 && result.Manifest.matches(task) && result.Manifest.validTimes(update.at) &&
		result.validArtifacts() && result.Manifest.validVerification(update.state)
}
func (manifest ResultManifest) matches(task FrozenTask) bool {
	raw, err := json.Marshal(manifest)
	return err == nil && len(raw) <= 32*1024 && manifest.validText() &&
		manifest.EnvelopeDigest == task.EnvelopeDigest && manifest.PromptDigest == task.Envelope.PromptDigest &&
		manifest.AdapterVersion == "stub-v1" && manifest.ProfileRevision == task.Envelope.ProfileRevision &&
		manifest.WorkspaceBaseline == task.Envelope.WorkspaceSnapshot && manifest.WorkspaceFinal.valid() && manifest.validFiles()
}
func (manifest ResultManifest) validFiles() bool {
	if len(manifest.ChangedFiles) == 0 || len(manifest.ChangedFiles) > 16 {
		return false
	}
	for _, file := range manifest.ChangedFiles {
		if !strings.HasPrefix(file, "SIMULATED_") || strings.ContainsAny(file, "/\\") || len(file) > 128 || !safeText(file) {
			return false
		}
	}
	return len(manifest.Unresolved) <= 16 && len(manifest.PartialEffects) <= 16 && len(manifest.TerminationEvidence) <= 1024
}
func (manifest ResultManifest) validTimes(observed string) bool {
	start, err := time.Parse(time.RFC3339Nano, manifest.StartedAt)
	end, endErr := time.Parse(time.RFC3339Nano, manifest.EndedAt)
	at, atErr := time.Parse(time.RFC3339Nano, observed)
	return err == nil && endErr == nil && atErr == nil && !end.Before(start) && !end.After(at) &&
		manifest.TerminationEvidence == "scripted fixture termination; no process launched"
}
func (result AdapterResult) validArtifacts() bool {
	if len(result.Artifacts) == 0 || len(result.Artifacts) > 4 || len(result.Manifest.Artifacts) != len(result.Artifacts) {
		return false
	}
	seen := permissions{}
	for i, artifact := range result.Artifacts {
		if seen[artifact.ID] || !artifact.valid() || artifact.metadata() != result.Manifest.Artifacts[i] {
			return false
		}
		seen[artifact.ID] = true
	}
	return result.Manifest.DiffDigest == result.Artifacts[0].Digest
}
func (artifact ResultArtifact) valid() bool {
	return artifact.Simulated && boundedSummary(artifact.Summary) && labelled(artifact.Text) && len(artifact.Text) <= MaxDraftBytes &&
		idPattern.MatchString(artifact.ID) && strings.HasPrefix(artifact.ID, "SIMULATED_") &&
		strings.HasPrefix(artifact.Filename, "SIMULATED_") && !strings.ContainsAny(artifact.Filename, "/\\") &&
		len(artifact.Filename) <= 128 && safeText(artifact.Filename) && safeText(artifact.Text) && artifact.Digest == byteDigest([]byte(artifact.Text))
}
func (manifest ResultManifest) validVerification(state string) bool {
	if len(manifest.Verification) == 0 || len(manifest.Verification) > 16 {
		return false
	}
	failed := false
	for _, evidence := range manifest.Verification {
		if !evidence.valid() {
			return false
		}
		failed = failed || evidence.Outcome == "failed"
	}
	return (state == "failed") == failed
}
func (evidence VerificationEvidence) valid() bool {
	return evidence.Simulated && boundedSummary(evidence.Summary) && evidence.validOutput() &&
		(evidence.Outcome == "passed" || evidence.Outcome == "failed")
}

func boundedSummary(text string) bool { return labelled(text) && len(text) <= 1024 && safeText(text) }
func boundedNotes(notes []string) bool {
	if len(notes) > 16 {
		return false
	}
	for _, note := range notes {
		if len(note) > 1024 || !safeText(note) {
			return false
		}
	}
	return true
}
func (manifest ResultManifest) validText() bool {
	return manifest.Simulated && boundedSummary(manifest.Summary) && boundedNotes(manifest.Unresolved) && boundedNotes(manifest.PartialEffects)
}
func (evidence VerificationEvidence) validOutput() bool {
	return evidence.Command == "SIMULATED_fixture_check" && boundedSummary(evidence.Output) && evidence.OutputDigest == byteDigest([]byte(evidence.Output))
}
