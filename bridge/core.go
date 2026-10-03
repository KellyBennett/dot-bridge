// Package bridge implements an offline synthetic contract and receipt journal.
// It provides no transport, execution adapter, credentials or filesystem writes.
package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
	"unicode/utf8"
)

const Label = "SIMULATED — NO REAL AGENT EXECUTION"
const MaxRequestBytes = 8 * 1024 // This slice accepts read selectors only.
const MaxDraftBytes = 128 * 1024

var (
	idPattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	revisionPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	operations      = map[string]bool{"read_draft": true, "write_draft": true,
		"submit_task": true, "read_run": true, "cancel_run": true, "read_pr": true}
)

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

type request struct {
	ContractVersion string          `json:"contract_version"`
	RequestID       string          `json:"request_id"`
	Operation       string          `json:"operation"`
	ProjectID       string          `json:"project_id"`
	Arguments       json.RawMessage `json:"arguments"`
}

type readDraftArgs struct {
	DocumentID       string  `json:"document_id"`
	ExpectedRevision *string `json:"expected_revision,omitempty"`
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
	journal     ReceiptRecorder
	grant       Grant
	permissions map[string]bool
	documents   map[string]string
	registered  map[string]bool
	clock       func() time.Time
	exportCheck func(string) (bool, error)
}

func NewBroker(journal ReceiptRecorder, config Config) (*Broker, error) {
	g := config.Grant
	if journal == nil || g.PrincipalID == "" || g.Environment != "personal" ||
		!idPattern.MatchString(g.ProjectID) || !revisionPattern.MatchString(g.PolicyRevision) ||
		g.ExpiresAt.IsZero() {
		return nil, errors.New("invalid synthetic host grant")
	}
	b := &Broker{journal: journal, grant: g, permissions: map[string]bool{},
		documents: map[string]string{}, registered: map[string]bool{},
		clock: config.Clock, exportCheck: config.ExportCheck}
	for _, op := range g.Operations {
		if !operations[op] {
			return nil, errors.New("invalid host operation")
		}
		b.permissions[op] = true
	}
	for _, id := range g.DocumentIDs {
		if !idPattern.MatchString(id) {
			return nil, errors.New("invalid host document ID")
		}
		b.registered[id] = true
	}
	for id, text := range config.Documents {
		if !b.registered[id] || !utf8.ValidString(text) {
			return nil, errors.New("invalid synthetic document")
		}
		b.documents[id] = text
	}
	if b.clock == nil {
		b.clock = time.Now
	}
	return b, nil
}

func byteDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func strictDecode(raw []byte, target any, required []string, optional []string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return errors.New("object required")
	}
	allowed := map[string]bool{}
	for _, field := range required {
		value, exists := object[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("required field absent")
		}
		allowed[field] = true
	}
	for _, field := range optional {
		allowed[field] = true
	}
	for field, value := range object {
		if !allowed[field] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("unknown or null field")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// uniqueJSON rejects duplicate keys at every level and excessive nesting before
// Go's regular decoder can silently overwrite fields or accept ambiguous input.
func uniqueJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return errors.New("nesting limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						return err
					}
					name, ok := key.(string)
					if !ok || seen[name] {
						return errors.New("duplicate or invalid key")
					}
					seen[name] = true
					if err := walk(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for decoder.More() {
					if err := walk(depth + 1); err != nil {
						return err
					}
				}
			default:
				return errors.New("unexpected delimiter")
			}
			_, err = decoder.Token()
			return err
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func validateRequest(raw []byte) (request, readDraftArgs, string) {
	var r request
	var args readDraftArgs
	if !utf8.Valid(raw) || uniqueJSON(raw) != nil || strictDecode(raw, &r,
		[]string{"contract_version", "request_id", "operation", "project_id", "arguments"}, nil) != nil ||
		r.ContractVersion != "1" || !uuidPattern.MatchString(r.RequestID) ||
		!idPattern.MatchString(r.ProjectID) || !operations[r.Operation] {
		return r, args, "INVALID_ARGUMENT"
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(r.Arguments, &object) != nil || object == nil {
		return r, args, "INVALID_ARGUMENT"
	}
	if r.Operation != "read_draft" {
		return r, args, "OPERATION_NOT_IMPLEMENTED"
	}
	if strictDecode(r.Arguments, &args, []string{"document_id"}, []string{"expected_revision"}) != nil ||
		!idPattern.MatchString(args.DocumentID) ||
		(args.ExpectedRevision != nil && !revisionPattern.MatchString(*args.ExpectedRevision)) {
		return r, args, "INVALID_ARGUMENT"
	}
	return r, args, ""
}

func safeExport(check func(string) (bool, error), text string) (allowed bool, err error) {
	defer func() {
		if recover() != nil {
			allowed, err = false, errors.New("export validator unavailable")
		}
	}()
	if check == nil {
		return false, nil
	}
	return check(text)
}

// Dispatch returns only after the journal commits. Audit failure returns a zero
// response and ErrAuditUnavailable; callers must explicitly report no receipt.
// The only implemented effect is reading an inert synthetic fixture.
func (b *Broker) Dispatch(ctx context.Context, raw []byte, identity *Identity) (Response, error) {
	id, err := newUUID()
	if err != nil {
		return Response{}, ErrAuditUnavailable
	}
	now := b.clock().UTC()
	receipt := Receipt{ReceiptID: id, PolicyRevision: b.grant.PolicyRevision,
		InputDigest: byteDigest(raw), RecordedAt: now.Format(time.RFC3339Nano), Simulated: true}
	// Invalid caller fields are omitted, never echoed verbatim into audit metadata.
	var metadata map[string]json.RawMessage
	if len(raw) <= MaxRequestBytes && utf8.Valid(raw) && uniqueJSON(raw) == nil &&
		json.Unmarshal(raw, &metadata) == nil {
		var value string
		if json.Unmarshal(metadata["request_id"], &value) == nil && uuidPattern.MatchString(value) {
			receipt.RequestID = value
		}
		if json.Unmarshal(metadata["operation"], &value) == nil && operations[value] {
			receipt.Operation = value
		}
		if json.Unmarshal(metadata["project_id"], &value) == nil && idPattern.MatchString(value) {
			receipt.ProjectID = value
		}
	}
	if identity != nil {
		receipt.PrincipalID, receipt.Environment = identity.PrincipalID, identity.Environment
	}
	var data *DraftData
	code := ""
	switch {
	case identity == nil:
		code = "UNAUTHENTICATED"
	case identity.PrincipalID != b.grant.PrincipalID || identity.Environment != b.grant.Environment:
		code = "DENIED"
	case !b.grant.Enabled:
		code = "PROJECT_DISABLED"
	case !now.Before(b.grant.ExpiresAt):
		code = "DENIED"
	case len(raw) > MaxRequestBytes:
		code = "LIMIT_EXCEEDED"
	default:
		var r request
		var args readDraftArgs
		r, args, code = validateRequest(raw)
		if code == "" {
			switch {
			case r.ProjectID != b.grant.ProjectID || !b.permissions[r.Operation]:
				code = "DENIED"
			case !b.registered[args.DocumentID]:
				code = "DENIED"
			default:
				text, exists := b.documents[args.DocumentID]
				rev := byteDigest([]byte(text))
				switch {
				case !exists:
					code = "NOT_FOUND"
				case len(text) > MaxDraftBytes:
					code = "LIMIT_EXCEEDED"
				case args.ExpectedRevision != nil && *args.ExpectedRevision != rev:
					code = "REVISION_MISMATCH"
				default:
					allowed, err := safeExport(b.exportCheck, text)
					if err != nil {
						code = "RESOURCE_UNAVAILABLE"
					} else if !allowed {
						code = "EXPORT_BLOCKED"
					} else {
						data = &DraftData{Summary: Label, Text: text, Revision: rev, ByteCount: len(text),
							DocumentID: args.DocumentID, Provenance: "host synthetic fixture; untrusted data", Simulated: true}
						receipt.DocumentID, receipt.DocumentRevision = args.DocumentID, rev
						receipt.OutputDigest, receipt.ExportedBytes = rev, len(text)
					}
				}
			}
		}
	}
	if code != "" {
		receipt.Decision, receipt.Effect, receipt.Outcome, receipt.ErrorCode = "denied", "none", "rejected", code
	} else {
		receipt.Decision, receipt.Effect, receipt.Outcome = "allowed", "draft_read", "completed"
	}
	committed, err := b.journal.Record(ctx, receipt)
	if err != nil {
		return Response{}, err
	}
	return Response{Summary: Label, Simulated: true, Receipt: committed, Data: data}, nil
}
