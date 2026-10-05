package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ExecutionAdapter is host-owned. Version one workers accept only a simulated,
// profile-bound adapter with durable lookup. No adapter is a public operation.
type ExecutionAdapter interface {
	Capabilities() AdapterCapabilities
	Preflight(context.Context, FrozenTask) (string, error)
	Start(context.Context, DispatchInput) (StartObservation, error)
	Inspect(context.Context, InspectInput) (AdapterObservation, error)
	Collect(context.Context, string) (AdapterResult, error)
}
type AdapterCapabilities struct {
	Version               string
	Simulated             bool
	ProfileRevision       string
	DurableDispatchLookup bool
	StructuredEvents      bool
	Cancellation          bool
	ApprovalBehavior      string
}

func (c AdapterCapabilities) valid(profile string) bool {
	return c.Version == "stub-v1" && c.Simulated && c.ProfileRevision == profile &&
		c.DurableDispatchLookup && c.StructuredEvents && !c.Cancellation && c.ApprovalBehavior == "none"
}

type FrozenTask struct {
	Envelope       TaskEnvelope `json:"envelope"`
	EnvelopeDigest string       `json:"envelope_digest"`
	Specs          []FrozenSpec `json:"specs"`
}

func (review ApprovalReview) task() (FrozenTask, error) {
	envelope, err := parseEnvelope([]byte(review.CanonicalEnvelope))
	task := FrozenTask{Envelope: envelope, EnvelopeDigest: review.EnvelopeDigest, Specs: review.FrozenSpecs}
	if err != nil || !task.valid() {
		return FrozenTask{}, errors.New("invalid frozen task")
	}
	return task, nil
}
func (task FrozenTask) valid() bool {
	canonical, err := task.Envelope.canonical()
	return err == nil && task.Envelope.valid() && byteDigest(canonical) == task.EnvelopeDigest && task.validSpecs()
}
func (task FrozenTask) validSpecs() bool {
	if len(task.Specs) != len(task.Envelope.Specs) {
		return false
	}
	for i, spec := range task.Specs {
		if !spec.matches(task.Envelope.Specs[i]) {
			return false
		}
	}
	return true
}
func (spec FrozenSpec) matches(ref SpecReference) bool {
	return spec.DocumentID == ref.DocumentID && spec.Revision == ref.Revision &&
		len(spec.Text) <= MaxDraftBytes && byteDigest([]byte(spec.Text)) == ref.Revision
}

type DispatchInput struct {
	Token     string
	Task      FrozenTask
	StartedAt string
}
type InspectInput struct {
	Token         string
	AfterSequence int64
	Now           string
}
type StartObservation struct {
	Status    string
	ErrorCode string
}
type AdapterObservation struct {
	State          string
	Events         []AdapterEvent
	LastVerifiedAt string
	ErrorCode      string
}
type AdapterEvent struct {
	Sequence  int64  `json:"sequence"`
	At        string `json:"at"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Summary   string `json:"summary"`
	Simulated bool   `json:"simulated"`
}
type RunEvent struct {
	Sequence        int64  `json:"sequence"`
	AdapterSequence int64  `json:"adapter_sequence,omitempty"`
	At              string `json:"at"`
	Kind            string `json:"kind"`
	State           string `json:"state"`
	Summary         string `json:"summary"`
	Simulated       bool   `json:"simulated"`
}
type ResultArtifact struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Summary   string `json:"summary"`
	Text      string `json:"text"`
	Digest    string `json:"digest"`
	Simulated bool   `json:"simulated"`
}
type ArtifactMetadata struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Digest    string `json:"digest"`
	ByteCount int    `json:"byte_count"`
	Simulated bool   `json:"simulated"`
}
type VerificationEvidence struct {
	Command      string `json:"command"`
	Output       string `json:"output"`
	OutputDigest string `json:"output_digest"`
	Summary      string `json:"summary"`
	Outcome      string `json:"outcome"`
	Simulated    bool   `json:"simulated"`
}
type ResultManifest struct {
	Summary             string                 `json:"summary"`
	EnvelopeDigest      string                 `json:"envelope_digest"`
	PromptDigest        string                 `json:"prompt_digest"`
	AdapterVersion      string                 `json:"adapter_version"`
	ProfileRevision     string                 `json:"profile_revision"`
	WorkspaceBaseline   WorkspaceSnapshot      `json:"workspace_baseline"`
	WorkspaceFinal      WorkspaceSnapshot      `json:"workspace_final"`
	ChangedFiles        []string               `json:"changed_files"`
	DiffDigest          string                 `json:"diff_digest"`
	Verification        []VerificationEvidence `json:"verification"`
	Unresolved          []string               `json:"unresolved"`
	PartialEffects      []string               `json:"partial_effects"`
	StartedAt           string                 `json:"started_at"`
	EndedAt             string                 `json:"ended_at"`
	TerminationEvidence string                 `json:"termination_evidence"`
	Artifacts           []ArtifactMetadata     `json:"artifacts"`
	Simulated           bool                   `json:"simulated"`
}
type AdapterResult struct {
	Manifest  ResultManifest   `json:"manifest"`
	Artifacts []ResultArtifact `json:"artifacts"`
}

func (a ResultArtifact) metadata() ArtifactMetadata {
	return ArtifactMetadata{ID: a.ID, Filename: a.Filename, Digest: a.Digest, ByteCount: len(a.Text), Simulated: true}
}
func labelled(summary string) bool { return strings.HasPrefix(summary, Label) }
func (r AdapterResult) encode() (string, error) {
	raw, err := json.Marshal(r)
	return string(raw), err
}
