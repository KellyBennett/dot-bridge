// Package bridge implements an offline synthetic contract and durable receipts.
// It provides no transport, execution adapter or filesystem draft writes.
package bridge

import (
	"context"
	"time"
)

const Label = "SIMULATED — NO REAL AGENT EXECUTION"
const MaxRequestBytes = 8 * 1024
const MaxDraftBytes = 128 * 1024

// Identity is trusted host context. Never construct it from request fields.
type Identity struct {
	PrincipalID string
	Environment string
}

// Grant is host-owned configuration; this prototype accepts personal fixtures only.
type Grant struct {
	PrincipalID    string
	Environment    string
	ProjectID      string
	Operations     []string
	DocumentIDs    []string
	PolicyRevision string
	ExpiresAt      time.Time
	Enabled        bool
}

// Receipt excludes raw input, fixture text and internal error details.
type Receipt struct {
	ReceiptID        string `json:"receipt_id"`
	RequestID        string `json:"request_id,omitempty"`
	Operation        string `json:"operation,omitempty"`
	PrincipalID      string `json:"principal_id,omitempty"`
	Environment      string `json:"environment,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	PolicyRevision   string `json:"policy_revision"`
	InputDigest      string `json:"input_digest"`
	RecordedAt       string `json:"recorded_at"`
	Decision         string `json:"decision"`
	Effect           string `json:"effect"`
	Outcome          string `json:"outcome"`
	ErrorCode        string `json:"error_code,omitempty"`
	DocumentID       string `json:"document_id,omitempty"`
	DocumentRevision string `json:"document_revision,omitempty"`
	OutputDigest     string `json:"output_digest,omitempty"`
	ExportedBytes    int    `json:"exported_bytes,omitempty"`
	JournalSequence  int64  `json:"journal_sequence"`
	Simulated        bool   `json:"simulated"`
}

type DraftData struct {
	Summary    string `json:"summary"`
	Text       string `json:"text"`
	Revision   string `json:"revision"`
	ByteCount  int    `json:"byte_count"`
	DocumentID string `json:"document_id"`
	Provenance string `json:"provenance"`
	Simulated  bool   `json:"simulated"`
}

type Response struct {
	Summary   string     `json:"summary"`
	Simulated bool       `json:"simulated"`
	Receipt   Receipt    `json:"receipt"`
	Data      *DraftData `json:"data"`
	code      string
}

// Config is supplied locally by the host, never through the public contract.
// A nil ExportCheck denies all exports. Callbacks must be safe for concurrent use.
type Config struct {
	Grant       Grant
	Documents   map[string]string
	Clock       func() time.Time
	ExportCheck func(string) (bool, error)
}

// ReceiptRecorder returns success only after a durable commit.
type ReceiptRecorder interface {
	Record(context.Context, Receipt) (Receipt, error)
}

// Broker copies registry/grant collections at construction and has no listener.
type Broker struct {
	journal ReceiptRecorder
	policy  accessPolicy
	drafts  draftRegistry
	clock   func() time.Time
}
