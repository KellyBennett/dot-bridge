package bridge

import (
	"errors"
	"testing"
)

type exportScenario struct {
	check func(string) (bool, error)
	code  string
}

func exportScenarios() []exportScenario {
	return []exportScenario{
		{nil, "EXPORT_BLOCKED"},
		{func(string) (bool, error) { return false, nil }, "EXPORT_BLOCKED"},
		{func(string) (bool, error) { return true, errors.New("SEEDED_SECRET") }, "RESOURCE_UNAVAILABLE"},
		{func(string) (bool, error) { panic("SEEDED_SECRET") }, "RESOURCE_UNAVAILABLE"},
	}
}

func (f *bridgeFixture) exportScenario(s exportScenario) {
	f.config.ExportCheck = s.check
	f.rebuild()
	f.deny(nil, s.code).secretAbsent("SEEDED_SECRET")
}

func TestExportDefaultDenialErrorsAndPanicsAreSanitized(t *testing.T) {
	f := newFixture(t)
	f.text("SEEDED_SECRET")
	for _, scenario := range exportScenarios() {
		f.exportScenario(scenario)
	}
}
