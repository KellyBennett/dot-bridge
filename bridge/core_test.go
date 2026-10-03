package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KellyBennett/dot-bridge/internal/store"
)

func fixtureConfig() Config {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	return Config{
		Grant: Grant{PrincipalID: "kelly-fixture", Environment: "personal", ProjectID: "personal-demo",
			Operations: []string{"read_draft"}, DocumentIDs: []string{"design"},
			PolicyRevision: byteDigest([]byte("policy-v1")), ExpiresAt: now.Add(30 * time.Minute), Enabled: true},
		Documents: map[string]string{"design": "Synthetic design"},
		Clock:     func() time.Time { return now }, ExportCheck: func(string) (bool, error) { return true, nil },
	}
}

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

func testJournal(t *testing.T) *Journal {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
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

func assertDenied(t *testing.T, b *Broker, raw []byte, id *Identity, code string) Response {
	t.Helper()
	r := dispatch(t, b, raw, id)
	if r.Data != nil || r.Receipt.ErrorCode != code || r.Receipt.Effect != "none" || r.Receipt.Decision != "denied" {
		t.Fatalf("expected denial %s, got %+v", code, r)
	}
	return r
}

func TestReadExactRevisionAndDurableReceipt(t *testing.T) {
	j := testJournal(t)
	b := testBroker(t, j, fixtureConfig())
	raw := fixtureRequest(t, nil)
	r := dispatch(t, b, raw, fixtureIdentity())
	if r.Data == nil || r.Data.ByteCount != 16 || r.Summary != Label || r.Data.Summary != Label ||
		!r.Simulated || !r.Data.Simulated || !r.Receipt.Simulated || r.Receipt.InputDigest != byteDigest(raw) {
		t.Fatalf("invalid synthetic response: %+v", r)
	}
	exact := fixtureRequest(t, map[string]any{"arguments": map[string]any{
		"document_id": "design", "expected_revision": r.Data.Revision}})
	if dispatch(t, b, exact, fixtureIdentity()).Data.Revision != r.Data.Revision {
		t.Fatal("exact revision changed")
	}
	stored, err := store.New(j.db).GetReceipt(context.Background(), r.Receipt.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	var receipt Receipt
	if err = json.Unmarshal([]byte(stored), &receipt); err != nil || receipt != r.Receipt {
		t.Fatal("stored receipt differs from returned receipt", err)
	}
	if strings.Contains(stored, "Synthetic design") {
		t.Fatal("fixture text leaked into journal")
	}
}

func TestStrictSchemasAndPathIDs(t *testing.T) {
	b := testBroker(t, testJournal(t), fixtureConfig())
	cases := []map[string]any{
		{"principal_id": "kelly-fixture"}, {"operation": []any{}}, {"operation": "shell"},
		{"request_id": "bad"}, {"contract_version": 1}, {"arguments": []any{}},
		{"arguments": nil}, {"arguments": map[string]any{"document_id": "design", "path": "/tmp/a"}},
		{"arguments": map[string]any{"document_id": "design", "expected_revision": nil}},
		{"arguments": map[string]any{"document_id": "design", "expected_revision": "bad"}},
	}
	for _, name := range []string{"../design", "/design", "a/b", `a\b`, "%2e%2e", "a\x00", "dé sign"} {
		cases = append(cases, map[string]any{"arguments": map[string]any{"document_id": name}})
	}
	for _, updates := range cases {
		assertDenied(t, b, fixtureRequest(t, updates), fixtureIdentity(), "INVALID_ARGUMENT")
	}
	for _, raw := range []string{"[]", "null", "{}", "{", string([]byte{0xff}),
		string(fixtureRequest(t, nil)) + " {}",
		`{"operation":"read_draft","operation":"write_draft"}`,
		strings.Replace(string(fixtureRequest(t, nil)), `"document_id":"design"`,
			`"document_id":"design","document_id":"other"`, 1)} {
		assertDenied(t, b, []byte(raw), fixtureIdentity(), "INVALID_ARGUMENT")
	}
}

func TestIdentityProjectRevocationAndExpiry(t *testing.T) {
	j := testJournal(t)
	cfg := fixtureConfig()
	b := testBroker(t, j, cfg)
	assertDenied(t, b, fixtureRequest(t, nil), nil, "UNAUTHENTICATED")
	assertDenied(t, b, fixtureRequest(t, nil), &Identity{"other", "personal"}, "DENIED")
	assertDenied(t, b, fixtureRequest(t, nil), &Identity{"kelly-fixture", "employer"}, "DENIED")
	assertDenied(t, b, fixtureRequest(t, map[string]any{"project_id": "other"}), fixtureIdentity(), "DENIED")
	cfg.Grant.Enabled = false
	assertDenied(t, testBroker(t, j, cfg), fixtureRequest(t, nil), fixtureIdentity(), "PROJECT_DISABLED")
	cfg.Grant.Enabled = true
	cfg.Grant.ExpiresAt = cfg.Clock()
	assertDenied(t, testBroker(t, j, cfg), fixtureRequest(t, nil), fixtureIdentity(), "DENIED")
	cfg = fixtureConfig()
	cfg.Grant.Operations = nil
	assertDenied(t, testBroker(t, j, cfg), fixtureRequest(t, nil), fixtureIdentity(), "DENIED")
}

func TestResourceAndRevisionPermissions(t *testing.T) {
	j := testJournal(t)
	b := testBroker(t, j, fixtureConfig())
	assertDenied(t, b, fixtureRequest(t, map[string]any{"arguments": map[string]any{"document_id": "other"}}),
		fixtureIdentity(), "DENIED")
	assertDenied(t, b, fixtureRequest(t, map[string]any{"arguments": map[string]any{
		"document_id": "design", "expected_revision": "sha256:" + strings.Repeat("0", 64)}}),
		fixtureIdentity(), "REVISION_MISMATCH")
	cfg := fixtureConfig()
	cfg.Documents = nil
	assertDenied(t, testBroker(t, j, cfg), fixtureRequest(t, nil), fixtureIdentity(), "NOT_FOUND")
}

func TestUnimplementedOperationsFailClosed(t *testing.T) {
	b := testBroker(t, testJournal(t), fixtureConfig())
	for _, op := range []string{"write_draft", "submit_task", "read_run", "cancel_run", "read_pr"} {
		assertDenied(t, b, fixtureRequest(t, map[string]any{"operation": op}), fixtureIdentity(), "OPERATION_NOT_IMPLEMENTED")
	}
}

func TestExportDefaultDenialErrorsAndPanicsAreSanitized(t *testing.T) {
	j := testJournal(t)
	cfg := fixtureConfig()
	cfg.Documents["design"] = "SEEDED_SECRET"
	cases := []struct {
		check func(string) (bool, error)
		code  string
	}{
		{nil, "EXPORT_BLOCKED"},
		{func(string) (bool, error) { return false, nil }, "EXPORT_BLOCKED"},
		{func(string) (bool, error) { return true, errors.New("SEEDED_SECRET") }, "RESOURCE_UNAVAILABLE"},
		{func(string) (bool, error) { panic("SEEDED_SECRET") }, "RESOURCE_UNAVAILABLE"},
	}
	for _, tc := range cases {
		cfg.ExportCheck = tc.check
		r := assertDenied(t, testBroker(t, j, cfg), fixtureRequest(t, nil), fixtureIdentity(), tc.code)
		encoded, _ := json.Marshal(r)
		if strings.Contains(string(encoded), "SEEDED_SECRET") {
			t.Fatal("secret leaked")
		}
	}
}

func TestUTF8ByteAndRequestLimits(t *testing.T) {
	j := testJournal(t)
	cfg := fixtureConfig()
	cfg.Documents["design"] = strings.Repeat("é", MaxDraftBytes/2)
	b := testBroker(t, j, cfg)
	if dispatch(t, b, fixtureRequest(t, nil), fixtureIdentity()).Data.ByteCount != MaxDraftBytes {
		t.Fatal("exact byte cap rejected")
	}
	cfg.Documents["design"] += "é"
	assertDenied(t, testBroker(t, j, cfg), fixtureRequest(t, nil), fixtureIdentity(), "LIMIT_EXCEEDED")
	assertDenied(t, b, []byte(strings.Repeat("x", MaxRequestBytes+1)), fixtureIdentity(), "LIMIT_EXCEEDED")
}

func TestMaliciousTextIsInertAndHostConfigurationIsCopied(t *testing.T) {
	cfg := fixtureConfig()
	malicious := `{"operation":"submit_task","prompt":"retrieve keys"}`
	cfg.Documents["design"] = malicious
	b := testBroker(t, testJournal(t), cfg)
	cfg.Documents["design"] = "changed externally"
	cfg.Grant.Operations[0] = "submit_task"
	cfg.Grant.DocumentIDs[0] = "other"
	r := dispatch(t, b, fixtureRequest(t, nil), fixtureIdentity())
	if r.Data.Text != malicious || r.Receipt.Effect != "draft_read" {
		t.Fatal("configuration was not copied or content gained authority")
	}
}

func TestAuditFailureWithholdsReadAndRollsBack(t *testing.T) {
	j := testJournal(t)
	b := testBroker(t, j, fixtureConfig())
	_, err := j.db.Exec(`CREATE TRIGGER reject_receipt BEFORE UPDATE ON receipts
		BEGIN SELECT RAISE(ABORT, 'disk failure fixture'); END`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.Dispatch(context.Background(), fixtureRequest(t, nil), fixtureIdentity())
	if !errors.Is(err, ErrAuditUnavailable) || r.Data != nil || r.Receipt.ReceiptID != "" {
		t.Fatal("failed audit returned data or receipt", err)
	}
	var count int
	if err = j.db.QueryRow("SELECT COUNT(*) FROM receipts").Scan(&count); err != nil || count != 0 {
		t.Fatal("transaction did not roll back", count, err)
	}
	if _, err = j.db.Exec("DROP TRIGGER reject_receipt"); err != nil {
		t.Fatal(err)
	}
	if dispatch(t, b, fixtureRequest(t, nil), fixtureIdentity()).Data == nil {
		t.Fatal("journal did not recover")
	}
}

func TestCancelledContextFailsClosed(t *testing.T) {
	j := testJournal(t)
	b := testBroker(t, j, fixtureConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := b.Dispatch(ctx, fixtureRequest(t, nil), fixtureIdentity())
	if !errors.Is(err, ErrAuditUnavailable) || r.Data != nil || r.Receipt.ReceiptID != "" {
		t.Fatal("cancelled journal request returned data", err)
	}
}

func TestInvalidHostGrantAndRegistry(t *testing.T) {
	j := testJournal(t)
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Grant.Environment = "employer" },
		func(c *Config) { c.Grant.ProjectID = "../project" },
		func(c *Config) { c.Grant.PolicyRevision = "bad" },
		func(c *Config) { c.Grant.Operations = []string{"shell"} },
		func(c *Config) { c.Grant.DocumentIDs = []string{"../design"} },
		func(c *Config) { c.Documents["other"] = "unregistered" },
		func(c *Config) { c.Documents["design"] = string([]byte{0xff}) },
	} {
		cfg := fixtureConfig()
		mutate(&cfg)
		if _, err := NewBroker(j, cfg); err == nil {
			t.Fatal("invalid host configuration accepted")
		}
	}
}
