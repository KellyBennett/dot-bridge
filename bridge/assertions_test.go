package bridge

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// responseAssertion checks the public contract independently of storage probes.
type responseAssertion struct {
	t        *testing.T
	response Response
}

func (a responseAssertion) denied(code string) {
	a.t.Helper()
	r := a.response
	if r.Data != nil || r.Receipt.ErrorCode != code || r.Receipt.Effect != "none" || r.Receipt.Decision != "denied" {
		a.t.Fatalf("expected denial %s, got %+v", code, r)
	}
}

func (a responseAssertion) allowed() {
	a.t.Helper()
	if a.response.Data == nil || a.response.Receipt.Effect != "draft_read" {
		a.t.Fatalf("read rejected: %+v", a.response)
	}
}

func (a responseAssertion) simulated() {
	a.t.Helper()
	r := a.response
	if r.Summary != Label || !r.Simulated || !r.Receipt.Simulated {
		a.t.Fatal("response lacks simulation markers")
	}
	if r.Data == nil || r.Data.Summary != Label || !r.Data.Simulated {
		a.t.Fatal("data lacks simulation markers")
	}
}

func (a responseAssertion) bytes(count int) {
	a.allowed()
	if a.response.Data.ByteCount != count {
		a.t.Fatal("incorrect UTF-8 byte count")
	}
}

func (a responseAssertion) revision() string {
	a.allowed()
	return a.response.Data.Revision
}
func (a responseAssertion) receipt() Receipt { return a.response.Receipt }

func (a responseAssertion) sameRevision(expected string) {
	if a.revision() != expected {
		a.t.Fatal("exact revision changed")
	}
}

func (a responseAssertion) input(raw []byte) {
	if a.response.Receipt.InputDigest != byteDigest(raw) {
		a.t.Fatal("input digest changed")
	}
}

func (a responseAssertion) inertText(expected string) {
	a.allowed()
	if a.response.Data.Text != expected {
		a.t.Fatal("fixture content changed")
	}
}

func (a responseAssertion) secretAbsent(secret string) {
	encoded, err := json.Marshal(a.response)
	if err != nil || strings.Contains(string(encoded), secret) {
		a.t.Fatal("response leaked fixture secret", err)
	}
}

func (a responseAssertion) withheld(err error) {
	r := a.response
	if !errors.Is(err, ErrAuditUnavailable) || r.Data != nil || r.Receipt.ReceiptID != "" {
		a.t.Fatal("audit failure returned data or receipt", err)
	}
}
