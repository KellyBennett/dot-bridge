package bridge

import (
	"context"
	"errors"
	"time"
)

func NewBroker(journal ReceiptRecorder, c Config) (*Broker, error) {
	if journal == nil {
		return nil, errors.New("journal required")
	}
	return c.broker(journal)
}
func (c Config) broker(journal ReceiptRecorder) (*Broker, error) {
	b := &Broker{journal: journal, clock: c.Clock}
	if err := b.configure(c); err != nil {
		return nil, err
	}
	if err := b.configureTasks(c.Tasks); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Broker) configure(c Config) error {
	if err := b.policy.configure(c.Grant); err != nil {
		return err
	}
	if err := b.drafts.configure(c, b.policy.documents); err != nil {
		return err
	}
	return nil
}

func (b *Broker) now() time.Time {
	if b.clock == nil {
		return time.Now().UTC()
	}
	return b.clock().UTC()
}

// Dispatch returns reads and task acceptance only after their durable receipt commits.
func (b *Broker) Dispatch(ctx context.Context, raw []byte, identity *Identity) (Response, error) {
	receipt, err := b.receipt(raw, identity)
	if err != nil {
		return Response{}, err
	}
	if b.authorize(identity) == "" {
		if response, handled, err := b.dispatchTask(ctx, raw, receipt); handled {
			return response, err
		}
	}
	response := b.investigate(raw, identity)
	response.Receipt = receipt
	return response.commit(ctx, b.journal)
}

func (b *Broker) authorize(identity *Identity) string {
	return b.policy.authorize(identity, b.now())
}

func (b *Broker) investigate(raw []byte, identity *Identity) Response {
	if code := b.authorize(identity); code != "" {
		return Response{Summary: Label, Simulated: true, code: code}
	}
	data, code := b.read(raw)
	return Response{Summary: Label, Simulated: true, Data: data, code: code}
}

func (b *Broker) read(raw []byte) (*DraftData, string) {
	r, args, code := validateRequest(raw)
	if code != "" {
		return nil, code
	}
	if !b.policy.allows(r) {
		return nil, "DENIED"
	}
	return b.drafts.read(args)
}
