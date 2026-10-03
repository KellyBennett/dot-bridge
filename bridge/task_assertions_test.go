package bridge

import (
	"encoding/json"
	"strings"
)

func (f *taskFixture) assertFrozenReview(review ApprovalReview) {
	if len(review.FrozenSpecs) != 1 || review.FrozenSpecs[0].Text != f.config.Documents["design"] {
		f.t.Fatal("host review omitted frozen source")
	}
}
func (a taskAssertion) acceptedEvidence(approval Approval, review ApprovalReview) {
	run := a.run()
	if run.EnvelopeDigest != review.EnvelopeDigest || a.response.Replayed {
		a.t.Fatal("acceptance identity differs")
	}
	a.receiptEvidence(approval)
	a.privatePayloadAbsent(review)
}
func (a taskAssertion) privatePayloadAbsent(review ApprovalReview) {
	encoded := string(testJSON(a.t, a.response))
	if strings.Contains(encoded, review.FrozenSpecs[0].Text) || strings.Contains(encoded, "Synthetic task") {
		a.t.Fatal("public response leaked host payload")
	}
}
func (f *taskFixture) formattedReplay(approval Approval) taskAssertion {
	raw := f.formattedSubmission(approval)
	result := f.result(raw, fixtureIdentity())
	result.exactInput(raw)
	result.persisted(f)
	return result
}
func (f *taskFixture) formattedSubmission(approval Approval) []byte {
	var object map[string]any
	if err := json.Unmarshal(f.submission(approval), &object); err != nil {
		f.t.Fatal(err)
	}
	object["request_id"] = "00000000-0000-4000-8000-000000000099"
	raw, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	return raw
}
func (f *taskFixture) missingApproval() taskAssertion { return f.accepted(Approval{ApprovalID: f.key}) }
func (f *taskFixture) fillQueue() {
	for i := 0; i < 5; i++ {
		f.useKey("00000000-0000-4000-8000-00000000000" + string(rune('4'+i)))
		f.accepted(f.approve()).run()
	}
}
func (a taskAssertion) readEvidence(expected RunData) {
	a.sameRun(expected)
	r := a.response.Receipt
	if r.Effect != "run_read" || r.OutputDigest == "" || r.ExportedBytes == 0 {
		a.t.Fatal("run read evidence differs")
	}
}
