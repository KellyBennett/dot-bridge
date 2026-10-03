package bridge

import (
	"context"
	"encoding/json"
	"time"
)

func (r Response) commit(ctx context.Context, journal ReceiptRecorder) (Response, error) {
	r.Receipt.decide(r.Data, r.code)
	committed, err := journal.Record(ctx, r.Receipt)
	if err != nil {
		return Response{}, err
	}
	r.Receipt = committed
	return r, nil
}

func (b *Broker) receipt(raw []byte, identity *Identity) (Receipt, error) {
	id, err := newUUID()
	if err != nil {
		return Receipt{}, ErrAuditUnavailable
	}
	r := Receipt{ReceiptID: id, PolicyRevision: b.policy.grant.PolicyRevision,
		InputDigest: byteDigest(raw), RecordedAt: b.now().Format(time.RFC3339Nano), Simulated: true}
	r.identify(identity)
	r.metadata(raw)
	return r, nil
}

func (r *Receipt) identify(identity *Identity) {
	if identity != nil {
		r.PrincipalID, r.Environment = identity.PrincipalID, identity.Environment
	}
}

func (r *Receipt) metadata(raw []byte) {
	object, err := validatedObject(raw)
	if err != nil {
		return
	}
	r.RequestID = object.identifier("request_id", uuidPattern.MatchString)
	r.Operation = object.identifier("operation", func(s string) bool { return operations[s] })
	r.ProjectID = object.identifier("project_id", idPattern.MatchString)
}

func (r *Receipt) decide(data *DraftData, code string) {
	if code != "" {
		r.Decision, r.Effect, r.Outcome, r.ErrorCode = "denied", "none", "rejected", code
		return
	}
	r.Decision, r.Effect, r.Outcome = "allowed", "draft_read", "completed"
	r.DocumentID, r.DocumentRevision = data.DocumentID, data.Revision
	r.OutputDigest, r.ExportedBytes = data.Revision, data.ByteCount
}

type jsonObject map[string]json.RawMessage

func (o jsonObject) identifier(field string, valid func(string) bool) string {
	var value string
	if json.Unmarshal(o[field], &value) != nil || !valid(value) {
		return ""
	}
	return value
}
