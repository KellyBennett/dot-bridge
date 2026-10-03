package bridge

import (
	"path/filepath"
	"testing"
	"time"
)

func fixtureTime() time.Time { return time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) }

func fixtureGrant(now time.Time) Grant {
	return Grant{PrincipalID: "kelly-fixture", Environment: "personal", ProjectID: "personal-demo",
		Operations: []string{"read_draft"}, DocumentIDs: []string{"design"}, PolicyRevision: byteDigest([]byte("policy-v1")),
		ExpiresAt: now.Add(30 * time.Minute), Enabled: true}
}

func fixtureConfig() Config {
	now := fixtureTime()
	return Config{Grant: fixtureGrant(now), Documents: map[string]string{"design": "Synthetic design"},
		Clock: func() time.Time { return now }, ExportCheck: func(string) (bool, error) { return true, nil }}
}

// bridgeFixture owns a synthetic host configuration and its journal lifetime.
type bridgeFixture struct {
	t       *testing.T
	path    string
	journal *Journal
	config  Config
	broker  *Broker
}

func newFixture(t *testing.T) *bridgeFixture {
	f := &bridgeFixture{t: t, config: fixtureConfig()}
	f.path = filepath.Join(t.TempDir(), "journal.sqlite3")
	f.open()
	f.rebuild()
	t.Cleanup(f.close)
	return f
}

func (f *bridgeFixture) open() {
	var err error
	f.journal, err = OpenJournal(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *bridgeFixture) close() {
	if f.journal != nil {
		_ = f.journal.Close()
	}
}

func (f *bridgeFixture) restart() {
	f.close()
	f.open()
	f.rebuild()
}

func (f *bridgeFixture) rebuild() { f.broker = testBroker(f.t, f.journal, f.config) }

func (f *bridgeFixture) read(updates map[string]any) responseAssertion {
	return f.invoke(fixtureRequest(f.t, updates), fixtureIdentity())
}

func (f *bridgeFixture) invoke(raw []byte, identity *Identity) responseAssertion {
	return responseAssertion{t: f.t, response: dispatch(f.t, f.broker, raw, identity)}
}

func (f *bridgeFixture) deny(updates map[string]any, code string) responseAssertion {
	r := f.read(updates)
	r.denied(code)
	return r
}

func (f *bridgeFixture) text(text string) {
	f.config.Documents["design"] = text
	f.rebuild()
}

func (f *bridgeFixture) probe() *journalProbe { return &journalProbe{t: f.t, journal: f.journal} }

func (f *bridgeFixture) selector(id string) map[string]any {
	return map[string]any{"arguments": map[string]any{"document_id": id}}
}

func (f *bridgeFixture) exact(revision string) responseAssertion {
	return f.read(map[string]any{"arguments": map[string]any{"document_id": "design", "expected_revision": revision}})
}
