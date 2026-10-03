package bridge

import "testing"

func TestUnauthenticated(t *testing.T) {
	f := newFixture(t)
	f.invoke(fixtureRequest(t, nil), nil).denied("UNAUTHENTICATED")
}

func TestIdentityIsolation(t *testing.T) {
	f := newFixture(t)
	f.invoke(fixtureRequest(t, nil), &Identity{"other", "personal"}).denied("DENIED")
	f.invoke(fixtureRequest(t, nil), &Identity{"kelly-fixture", "employer"}).denied("DENIED")
}

func TestProjectIsolation(t *testing.T) {
	f := newFixture(t)
	f.deny(map[string]any{"project_id": "other"}, "DENIED")
}

func TestProjectDisabled(t *testing.T) {
	f := newFixture(t)
	f.config.Grant.Enabled = false
	f.rebuild()
	f.deny(nil, "PROJECT_DISABLED")
}

func TestGrantExpiry(t *testing.T) {
	f := newFixture(t)
	f.expire()
	f.rebuild()
	f.deny(nil, "DENIED")
}

func (f *bridgeFixture) expire() { f.config.Grant.ExpiresAt = f.config.Clock() }

func TestOperationGrantRevoked(t *testing.T) {
	f := newFixture(t)
	f.config.Grant.Operations = nil
	f.rebuild()
	f.deny(nil, "DENIED")
}

func invalidHostConfigurations() []func(*Config) {
	return []func(*Config){
		func(c *Config) { c.Grant.Environment = "employer" }, func(c *Config) { c.Grant.ProjectID = "../project" },
		func(c *Config) { c.Grant.PolicyRevision = "bad" }, func(c *Config) { c.Grant.Operations = []string{"shell"} },
		func(c *Config) { c.Grant.DocumentIDs = []string{"../design"} }, func(c *Config) { c.Documents["other"] = "unregistered" },
		func(c *Config) { c.Documents["design"] = string([]byte{0xff}) },
	}
}

func (f *bridgeFixture) rejectsConfiguration() {
	if _, err := NewBroker(f.journal, f.config); err == nil {
		f.t.Fatal("invalid host configuration accepted")
	}
}

func TestInvalidHostGrantAndRegistry(t *testing.T) {
	f := newFixture(t)
	for _, mutate := range invalidHostConfigurations() {
		f.config = fixtureConfig()
		mutate(&f.config)
		f.rejectsConfiguration()
	}
}
