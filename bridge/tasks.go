package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// TaskConfig registers one host-owned synthetic workspace and profile.
// It is copied at construction. Rebuild the broker to change host policy.
type TaskConfig struct {
	WorkspaceID          string
	Snapshot             WorkspaceSnapshot
	ProfileRevision      string
	ExportPolicyRevision string
}

type taskService struct {
	config  TaskConfig
	journal *Journal
	policy  accessPolicy
	drafts  draftRegistry
	clock   func() time.Time
}

func (c TaskConfig) valid() bool {
	return idPattern.MatchString(c.WorkspaceID) && c.Snapshot.valid() &&
		revisionPattern.MatchString(c.ProfileRevision) && revisionPattern.MatchString(c.ExportPolicyRevision)
}

func (b *Broker) configureTasks(config *TaskConfig) error {
	if config == nil {
		return nil
	}
	if !config.valid() {
		return errors.New("invalid synthetic task configuration")
	}
	return b.attachTasks(*config)
}

func (b *Broker) attachTasks(config TaskConfig) error {
	journal, err := b.taskJournal()
	if err != nil {
		return err
	}
	b.tasks = &taskService{config: config, journal: journal, clock: b.now}
	b.attachTaskAuthority()
	return nil
}
func (b *Broker) taskJournal() (*Journal, error) {
	journal, ok := b.journal.(*Journal)
	if !ok {
		return nil, errors.New("task acceptance requires a transactional journal")
	}
	return journal, nil
}
func (b *Broker) attachTaskAuthority() { b.tasks.configureAuthority(b.policy, b.drafts) }

func (s *taskService) configureAuthority(policy accessPolicy, drafts draftRegistry) {
	s.policy, s.drafts = policy, drafts
}

func (s *taskService) authority(e TaskEnvelope) string {
	if code := e.grantAuthority(s.policy.grant); code != "" {
		return code
	}
	if code := e.workspaceAuthority(s.config); code != "" {
		return code
	}
	return e.specAuthority(s.drafts)
}

func (e TaskEnvelope) grantAuthority(grant Grant) string {
	if e.ProjectID != grant.ProjectID || e.Environment != grant.Environment {
		return "DENIED"
	}
	if e.PolicyRevision != grant.PolicyRevision {
		return "APPROVAL_STALE"
	}
	return ""
}

func (e TaskEnvelope) workspaceAuthority(config TaskConfig) string {
	if e.WorkspaceID != config.WorkspaceID {
		return "DENIED"
	}
	if e.WorkspaceSnapshot != config.Snapshot {
		return "WORKSPACE_CHANGED"
	}
	if e.ProfileRevision != config.ProfileRevision || e.ExportPolicyRevision != config.ExportPolicyRevision {
		return "APPROVAL_STALE"
	}
	return ""
}

func (e TaskEnvelope) specAuthority(drafts draftRegistry) string {
	for _, ref := range e.Specs {
		if code := ref.authority(drafts); code != "" {
			return code
		}
	}
	return ""
}

func (ref SpecReference) authority(drafts draftRegistry) string {
	text, code := drafts.resolve(ref.DocumentID)
	if code != "" {
		return code
	}
	if len(text) > MaxDraftBytes {
		return "LIMIT_EXCEEDED"
	}
	if byteDigest([]byte(text)) != ref.Revision {
		return "APPROVAL_STALE"
	}
	return ""
}

// RunData exports only bounded acceptance metadata, never prompts or spec text.
type RunData struct {
	Summary         string `json:"summary"`
	RunID           string `json:"run_id"`
	EnvelopeDigest  string `json:"envelope_digest"`
	AcceptedAt      string `json:"accepted_at"`
	StartDeadline   string `json:"start_deadline"`
	State           string `json:"state"`
	StateVersion    int64  `json:"state_version"`
	LastVerifiedAt  string `json:"last_verified_at"`
	ResultAvailable bool   `json:"result_available"`
	Simulated       bool   `json:"simulated"`
}

type submitTaskArgs struct {
	Envelope       json.RawMessage `json:"envelope"`
	ApprovalID     string          `json:"approval_id"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type readRunArgs struct {
	RunID          string `json:"run_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func parseSubmission(raw []byte) (submitTaskArgs, TaskEnvelope, string) {
	args, err := decodeSubmission(raw)
	if err != nil || !args.valid() {
		return args, TaskEnvelope{}, "INVALID_ARGUMENT"
	}
	e, err := parseEnvelope(args.Envelope)
	if err != nil {
		return args, e, "INVALID_ARGUMENT"
	}
	return args, e, ""
}

func decodeSubmission(raw []byte) (submitTaskArgs, error) {
	var args submitTaskArgs
	object, err := boundedObject(raw, MaxSubmissionBytes)
	if err != nil {
		return args, err
	}
	err = object.decode(raw, &args, []string{"envelope", "approval_id", "idempotency_key"}, nil)
	return args, err
}

func (args submitTaskArgs) valid() bool {
	return uuidPattern.MatchString(args.ApprovalID) && uuidPattern.MatchString(args.IdempotencyKey)
}

func parseRunSelector(raw []byte) (readRunArgs, string) {
	var args readRunArgs
	object, err := validatedObject(raw)
	if err != nil {
		return args, "INVALID_ARGUMENT"
	}
	err = object.decode(raw, &args, nil, []string{"run_id", "idempotency_key"})
	if err != nil || len(object) != 1 || !args.valid() {
		return args, "INVALID_ARGUMENT"
	}
	return args, ""
}

func (args readRunArgs) valid() bool {
	return validRunID(args.RunID) || uuidPattern.MatchString(args.IdempotencyKey)
}

func validRunID(id string) bool {
	return len(id) == 40 && id[:4] == "sim_" && uuidPattern.MatchString(id[4:])
}

func taskResponse(receipt Receipt, code string) Response {
	response := Response{Summary: Label, Simulated: true, Receipt: receipt, code: code}
	if code != "" {
		response.Receipt.decide(nil, code)
	}
	return response
}

type taskInvocation struct {
	request  request
	receipt  Receipt
	rawBytes int
}

func (b *Broker) dispatchTask(ctx context.Context, raw []byte, receipt Receipt) (Response, bool, error) {
	if b.tasks == nil {
		return Response{}, false, nil
	}
	r, err := parseTaskRequest(raw)
	if err != nil || !r.taskOperation() {
		return Response{}, false, nil
	}
	call := taskInvocation{request: r, receipt: receipt, rawBytes: len(raw)}
	response, err := b.tasks.dispatch(ctx, call)
	return response, true, err
}

func (r request) taskOperation() bool {
	return r.Operation == "submit_task" || r.Operation == "read_run"
}

func (s *taskService) dispatch(ctx context.Context, call taskInvocation) (Response, error) {
	call.identify()
	if !s.policy.allows(call.request) {
		return s.deny(ctx, call.receipt, "DENIED")
	}
	if call.request.Operation == "submit_task" {
		return s.dispatchSubmission(ctx, call)
	}
	return s.dispatchRunRead(ctx, call)
}

func (call *taskInvocation) identify() {
	call.receipt.RequestID, call.receipt.Operation, call.receipt.ProjectID = call.request.RequestID, call.request.Operation, call.request.ProjectID
}

func (call taskInvocation) submission() (submission, string) {
	args, e, code := call.request.submission()
	return submission{args: args, envelope: e, receipt: call.receipt}, code
}
func (s *taskService) dispatchSubmission(ctx context.Context, call taskInvocation) (Response, error) {
	input, code := call.submission()
	if code != "" {
		return s.deny(ctx, call.receipt, code)
	}
	return s.submitTask(ctx, input)
}

type runLookup struct {
	receipt Receipt
	args    readRunArgs
}

func (call taskInvocation) runLookup() (runLookup, string) {
	if call.rawBytes > MaxRequestBytes {
		return runLookup{}, "LIMIT_EXCEEDED"
	}
	args, code := parseRunSelector(call.request.Arguments)
	return runLookup{receipt: call.receipt, args: args}, code
}
func (s *taskService) dispatchRunRead(ctx context.Context, call taskInvocation) (Response, error) {
	lookup, code := call.runLookup()
	if code != "" {
		return s.deny(ctx, call.receipt, code)
	}
	return s.readRun(ctx, lookup)
}

func parseTaskRequest(raw []byte) (request, error) {
	var r request
	object, err := boundedObject(raw, MaxSubmissionBytes)
	if err != nil {
		return r, err
	}
	err = object.decode(raw, &r, []string{"contract_version", "request_id", "operation", "project_id", "arguments"}, nil)
	if err != nil || !r.valid() {
		return r, errors.New("invalid task request")
	}
	return r, nil
}

func (s *taskService) deny(ctx context.Context, receipt Receipt, code string) (Response, error) {
	return taskResponse(receipt, code).commit(ctx, s.journal)
}

func (r *Receipt) taskResult(run RunData, effect string) {
	r.Decision, r.Effect, r.Outcome = "allowed", effect, run.State
	r.RunID, r.EnvelopeDigest, r.StateVersion = run.RunID, run.EnvelopeDigest, run.StateVersion
}

func acceptedRun(digest string, now time.Time) (RunData, error) {
	id, err := newUUID()
	if err != nil {
		return RunData{}, err
	}
	return RunData{Summary: Label, RunID: "sim_" + id, EnvelopeDigest: digest,
		AcceptedAt: now.Format(time.RFC3339Nano), StartDeadline: now.Add(10 * time.Minute).Format(time.RFC3339Nano),
		State: "accepted", StateVersion: 1, LastVerifiedAt: now.Format(time.RFC3339Nano), Simulated: true}, nil
}

func (r request) submission() (submitTaskArgs, TaskEnvelope, string) {
	return parseSubmission(r.Arguments)
}
