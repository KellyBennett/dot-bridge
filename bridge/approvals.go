package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ApprovalReview is displayed by a trusted host before IssueApproval is called.
// It is never issued or accepted as authority through Dispatch.
type ApprovalReview struct {
	Summary           string       `json:"summary"`
	PrincipalID       string       `json:"principal_id"`
	CanonicalEnvelope string       `json:"canonical_envelope"`
	EnvelopeDigest    string       `json:"envelope_digest"`
	FrozenSpecs       []FrozenSpec `json:"frozen_specs"`
	Simulated         bool         `json:"simulated"`
}

type FrozenSpec struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
	Text       string `json:"text"`
}

type Approval struct {
	Summary        string  `json:"summary"`
	ApprovalID     string  `json:"approval_id"`
	EnvelopeDigest string  `json:"envelope_digest"`
	ExpiresAt      string  `json:"expires_at"`
	Receipt        Receipt `json:"receipt"`
	Simulated      bool    `json:"simulated"`
}

// ReviewTask freezes the exact envelope and registered specs for host-local review.
// Never expose this method as a public tool or export path.
func (b *Broker) ReviewTask(raw []byte) (ApprovalReview, error) {
	if b.tasks == nil {
		return ApprovalReview{}, errors.New("OPERATION_NOT_IMPLEMENTED")
	}
	return b.tasks.review(raw)
}

func (s *taskService) review(raw []byte) (ApprovalReview, error) {
	e, err := parseEnvelope(raw)
	if err != nil {
		return ApprovalReview{}, errors.New("INVALID_ARGUMENT")
	}
	if code := s.authority(e); code != "" {
		return ApprovalReview{}, errors.New(code)
	}
	review, err := e.hostReview(s.principal(), s.drafts)
	if err != nil {
		return ApprovalReview{}, err
	}
	return review, nil
}

func (s *taskService) principal() string { return s.policy.grant.PrincipalID }

func (e TaskEnvelope) hostReview(principal string, drafts draftRegistry) (ApprovalReview, error) {
	canonical, err := e.canonical()
	return ApprovalReview{Summary: Label, PrincipalID: principal, CanonicalEnvelope: string(canonical),
		EnvelopeDigest: byteDigest(canonical), FrozenSpecs: drafts.freeze(e.Specs), Simulated: true}, err
}

func (d draftRegistry) freeze(refs []SpecReference) []FrozenSpec {
	specs := []FrozenSpec{}
	for _, ref := range refs {
		specs = append(specs, FrozenSpec{DocumentID: ref.DocumentID, Revision: ref.Revision, Text: d.documents[ref.DocumentID]})
	}
	return specs
}

func (review ApprovalReview) valid() bool {
	return review.Simulated && review.Summary == Label &&
		review.EnvelopeDigest == byteDigest([]byte(review.CanonicalEnvelope))
}

func (s *taskService) validateReview(review ApprovalReview) string {
	if !review.valid() {
		return "INVALID_ARGUMENT"
	}
	current, err := s.review([]byte(review.CanonicalEnvelope))
	if err != nil {
		return err.Error()
	}
	if !review.same(current) {
		return "APPROVAL_STALE"
	}
	return ""
}

func (review ApprovalReview) same(other ApprovalReview) bool {
	if review.PrincipalID != other.PrincipalID || review.CanonicalEnvelope != other.CanonicalEnvelope ||
		len(review.FrozenSpecs) != len(other.FrozenSpecs) {
		return false
	}
	for i, spec := range review.FrozenSpecs {
		if spec != other.FrozenSpecs[i] {
			return false
		}
	}
	return true
}

// IssueApproval records the owner's host-local approval of the entire exact review.
// A host must never expose this method as a dot-callable tool. Issuance and its
// attributable receipt commit together; approval is single-use for 30 minutes.
func (b *Broker) IssueApproval(ctx context.Context, review ApprovalReview) (Approval, error) {
	if b.tasks == nil {
		return Approval{}, errors.New("OPERATION_NOT_IMPLEMENTED")
	}
	return b.tasks.issue(ctx, review)
}

func (s *taskService) issue(ctx context.Context, review ApprovalReview) (Approval, error) {
	approved, err := s.prepareApproval(review)
	if err != nil {
		return Approval{}, err
	}
	return approved.commit(ctx, s.journal)
}
func (s *taskService) prepareApproval(review ApprovalReview) (approvedReview, error) {
	receipt, err := s.hostReceipt(review)
	if err != nil {
		return approvedReview{}, err
	}
	if code := s.approvalAuthority(review); code != "" {
		receipt.decide(nil, code)
		return approvedReview{receipt: receipt}, nil
	}
	approved, err := prepareApproval(review, receipt, s.clock())
	approved.validUntil, approved.clock = s.approvalDeadline(), s.clock
	return approved, err
}
func (a approvedReview) commit(ctx context.Context, journal *Journal) (Approval, error) {
	if a.receipt.ErrorCode != "" {
		return a.receipt.rejectApproval(ctx, journal, a.receipt.ErrorCode)
	}
	return journal.issueApproval(ctx, a)
}

func (s *taskService) approvalAuthority(review ApprovalReview) string {
	if code := s.policy.hostAuthority(s.clock()); code != "" {
		return code
	}
	return s.validateReview(review)
}

func (p accessPolicy) hostAuthority(now time.Time) string {
	identity := &Identity{PrincipalID: p.grant.PrincipalID, Environment: p.grant.Environment}
	if code := p.authorize(identity, now); code != "" {
		return code
	}
	if !p.operations["submit_task"] {
		return "DENIED"
	}
	return ""
}

func (s *taskService) hostReceipt(review ApprovalReview) (Receipt, error) {
	return s.policy.approvalReceipt([]byte(review.CanonicalEnvelope), s.clock())
}

func (p accessPolicy) approvalReceipt(raw []byte, now time.Time) (Receipt, error) {
	id, err := newUUID()
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	return Receipt{ReceiptID: id, Operation: "host_approve_task", ProjectID: p.grant.ProjectID,
		PrincipalID: p.grant.PrincipalID, Environment: p.grant.Environment, PolicyRevision: p.grant.PolicyRevision,
		InputDigest: byteDigest(raw), RecordedAt: now.Format(time.RFC3339Nano), Simulated: true}, nil
}

func (r Receipt) rejectApproval(ctx context.Context, journal *Journal, code string) (Approval, error) {
	r.decide(nil, code)
	recorded, err := journal.Record(ctx, r)
	if err != nil {
		return Approval{}, err
	}
	return Approval{Summary: Label, Receipt: recorded, Simulated: true}, errors.New(code)
}

type approvedReview struct {
	validUntil time.Time
	clock      func() time.Time
	review     ApprovalReview
	approval   Approval
	receipt    Receipt
}

func prepareApproval(review ApprovalReview, receipt Receipt, now time.Time) (approvedReview, error) {
	approval, err := newApproval(review.EnvelopeDigest, now)
	receipt.approve(approval)
	return approvedReview{review: review, approval: approval, receipt: receipt}, err
}

func newApproval(digest string, now time.Time) (Approval, error) {
	id, err := newUUID()
	return Approval{Summary: Label, ApprovalID: id, EnvelopeDigest: digest,
		ExpiresAt: now.Add(30 * time.Minute).Format(time.RFC3339Nano), Simulated: true}, err
}

func (r *Receipt) approve(approval Approval) {
	r.Decision, r.Effect, r.Outcome = "allowed", "approval_issued", "issued"
	r.ApprovalID, r.EnvelopeDigest = approval.ApprovalID, approval.EnvelopeDigest
}

func (review ApprovalReview) frozen() (string, error) {
	raw, err := json.Marshal(review)
	return string(raw), err
}

func (s *taskService) approvalDeadline() time.Time { return s.policy.grant.ExpiresAt }
func (a approvedReview) alive() bool               { return a.clock().Before(a.validUntil) }
