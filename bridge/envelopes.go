package bridge

import (
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"
)

const MaxPromptBytes = 32 * 1024

var commitPattern = regexp.MustCompile("^[0-9a-f]{40}([0-9a-f]{24})?$")

// WorkspaceSnapshot identifies an immutable synthetic workspace fixture.
type WorkspaceSnapshot struct {
	BaseCommit          string `json:"base_commit"`
	DirtyManifestDigest string `json:"dirty_manifest_digest"`
}

type SpecReference struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
}

// TaskEnvelope is the fixed v1 canonical field order. Arrays retain their order.
// PromptDigest covers decoded UTF-8 prompt bytes, including all whitespace.
type TaskEnvelope struct {
	CanonicalVersion     string            `json:"canonical_version"`
	TaskID               string            `json:"task_id"`
	ProjectID            string            `json:"project_id"`
	Environment          string            `json:"environment"`
	WorkspaceID          string            `json:"workspace_id"`
	WorkspaceSnapshot    WorkspaceSnapshot `json:"workspace_snapshot"`
	Prompt               string            `json:"prompt"`
	PromptDigest         string            `json:"prompt_digest"`
	Specs                []SpecReference   `json:"specs"`
	AgentProfileID       string            `json:"agent_profile_id"`
	ProfileRevision      string            `json:"profile_revision"`
	AllowedEffects       []string          `json:"allowed_effects"`
	ExportPolicyRevision string            `json:"export_policy_revision"`
	PolicyRevision       string            `json:"policy_revision"`
	AcceptanceCriteria   []string          `json:"acceptance_criteria"`
	Simulated            bool              `json:"simulated"`
}

var envelopeFields = []string{"canonical_version", "task_id", "project_id", "environment",
	"workspace_id", "workspace_snapshot", "prompt", "prompt_digest", "specs", "agent_profile_id",
	"profile_revision", "allowed_effects", "export_policy_revision", "policy_revision",
	"acceptance_criteria", "simulated"}

func parseEnvelope(raw []byte) (TaskEnvelope, error) {
	var e TaskEnvelope
	object, err := boundedObject(raw, MaxSubmissionBytes)
	if err != nil {
		return e, err
	}
	if err = object.decode(raw, &e, envelopeFields, nil); err != nil {
		return e, err
	}
	if err = envelopeChildren(object); err != nil || !e.valid() {
		return e, errors.New("invalid task envelope")
	}
	return e, nil
}

func envelopeChildren(object jsonObject) error {
	snapshot, err := boundedObject(object["workspace_snapshot"], MaxSubmissionBytes)
	if err != nil {
		return err
	}
	if err = snapshot.fields([]string{"base_commit", "dirty_manifest_digest"}, nil); err != nil {
		return err
	}
	return specChildren(object["specs"])
}

func specChildren(raw []byte) error {
	var refs []json.RawMessage
	if err := json.Unmarshal(raw, &refs); err != nil {
		return err
	}
	for _, raw := range refs {
		if err := specChild(raw); err != nil {
			return err
		}
	}
	return nil
}

func specChild(raw []byte) error {
	ref, err := boundedObject(raw, MaxSubmissionBytes)
	if err != nil {
		return err
	}
	return ref.fields([]string{"document_id", "revision"}, nil)
}

func (e TaskEnvelope) valid() bool {
	return e.CanonicalVersion == "1" && e.Simulated && e.Environment == "personal" &&
		uuidPattern.MatchString(e.TaskID) && idPattern.MatchString(e.ProjectID) &&
		idPattern.MatchString(e.WorkspaceID) && e.AgentProfileID == "stub-v1" &&
		e.WorkspaceSnapshot.valid() && e.validPrompt() && e.validRevisions() &&
		validSpecReferences(e.Specs) && validEffects(e.AllowedEffects) &&
		boundedStrings(e.AcceptanceCriteria, 16, 1024)
}

func (s WorkspaceSnapshot) valid() bool {
	return commitPattern.MatchString(s.BaseCommit) && revisionPattern.MatchString(s.DirtyManifestDigest)
}

func (e TaskEnvelope) validPrompt() bool {
	return len(e.Prompt) > 0 && len(e.Prompt) <= MaxPromptBytes && utf8.ValidString(e.Prompt) &&
		e.PromptDigest == byteDigest([]byte(e.Prompt))
}

func (e TaskEnvelope) validRevisions() bool {
	return revisionPattern.MatchString(e.ProfileRevision) &&
		revisionPattern.MatchString(e.ExportPolicyRevision) && revisionPattern.MatchString(e.PolicyRevision)
}

func validSpecReferences(refs []SpecReference) bool {
	if refs == nil || len(refs) > 16 {
		return false
	}
	seen := permissions{}
	for _, ref := range refs {
		if !idPattern.MatchString(ref.DocumentID) || !revisionPattern.MatchString(ref.Revision) || seen[ref.DocumentID] {
			return false
		}
		seen[ref.DocumentID] = true
	}
	return true
}

func validEffects(effects []string) bool {
	if effects == nil || len(effects) > 2 {
		return false
	}
	seen := permissions{}
	for _, effect := range effects {
		if (effect != "edit_workspace" && effect != "run_approved_verification") || seen[effect] {
			return false
		}
		seen[effect] = true
	}
	return true
}

func boundedStrings(values []string, count, bytes int) bool {
	if len(values) == 0 || len(values) > count {
		return false
	}
	for _, value := range values {
		if len(value) == 0 || len(value) > bytes || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

// canonical uses Go encoding/json's compact struct serialization and HTML escaping.
// Version 1 has no maps, omitted fields or number representations.
func (e TaskEnvelope) canonical() ([]byte, error) { return json.Marshal(e) }
