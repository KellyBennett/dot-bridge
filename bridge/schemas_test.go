package bridge

import (
	"strings"
	"testing"
)

func invalidRequestFields() []map[string]any {
	return []map[string]any{
		{"principal_id": "kelly-fixture"}, {"operation": []any{}}, {"operation": "shell"},
		{"request_id": "bad"}, {"contract_version": 1}, {"arguments": []any{}}, {"arguments": nil},
		{"arguments": map[string]any{"document_id": "design", "path": "/tmp/a"}},
		{"arguments": map[string]any{"document_id": "design", "expected_revision": nil}},
		{"arguments": map[string]any{"document_id": "design", "expected_revision": "bad"}},
	}
}

func TestStrictSchemas(t *testing.T) {
	f := newFixture(t)
	for _, updates := range invalidRequestFields() {
		f.deny(updates, "INVALID_ARGUMENT")
	}
}

func TestPathIDs(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"../design", "/design", "a/b", `a\b`, "%2e%2e", "a\x00", "dé sign"} {
		f.deny(f.selector(name), "INVALID_ARGUMENT")
	}
}

func TestMalformedJSON(t *testing.T) {
	f := newFixture(t)
	for _, raw := range []string{"[]", "null", "{}", "{", string([]byte{0xff}), `{"operation":"read_draft","operation":"write_draft"}`} {
		f.invoke([]byte(raw), fixtureIdentity()).denied("INVALID_ARGUMENT")
	}
}

func TestTrailingJSON(t *testing.T) {
	f := newFixture(t)
	f.invoke(append(fixtureRequest(t, nil), []byte(" {}")...), fixtureIdentity()).denied("INVALID_ARGUMENT")
}

func TestNestedDuplicateKeys(t *testing.T) {
	f := newFixture(t)
	raw := strings.Replace(string(fixtureRequest(t, nil)), `"document_id":"design"`, `"document_id":"design","document_id":"other"`, 1)
	f.invoke([]byte(raw), fixtureIdentity()).denied("INVALID_ARGUMENT")
}

func TestExcessiveNesting(t *testing.T) {
	f := newFixture(t)
	raw := strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)
	f.invoke([]byte(raw), fixtureIdentity()).denied("INVALID_ARGUMENT")
}

func TestUnimplementedOperationsFailClosed(t *testing.T) {
	f := newFixture(t)
	for _, op := range []string{"write_draft", "submit_task", "read_run", "cancel_run", "read_pr"} {
		f.deny(map[string]any{"operation": op}, "OPERATION_NOT_IMPLEMENTED")
	}
}
