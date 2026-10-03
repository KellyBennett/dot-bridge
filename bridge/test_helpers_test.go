package bridge

import (
	"context"
	"encoding/json"
	"testing"
)

func fixtureIdentity() *Identity {
	return &Identity{PrincipalID: "kelly-fixture", Environment: "personal"}
}

func fixtureRequest(t *testing.T, updates map[string]any) []byte {
	t.Helper()
	r := map[string]any{"contract_version": "1", "request_id": "a783e97c-a431-4ff4-af5c-a47170344c02",
		"operation": "read_draft", "project_id": "personal-demo", "arguments": map[string]any{"document_id": "design"}}
	for k, v := range updates {
		r[k] = v
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testBroker(t *testing.T, j ReceiptRecorder, cfg Config) *Broker {
	t.Helper()
	b, err := NewBroker(j, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dispatch(t *testing.T, b *Broker, raw []byte, id *Identity) Response {
	t.Helper()
	r, err := b.Dispatch(context.Background(), raw, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
