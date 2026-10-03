package bridge

import (
	"context"
	"sync"
	"testing"
)

type taskCall struct {
	response Response
	err      error
}

func concurrentSubmissions(brokers []*Broker, raw []byte) []taskCall {
	calls := make([]taskCall, len(brokers))
	var group sync.WaitGroup
	for i, broker := range brokers {
		group.Add(1)
		go func(i int, broker *Broker) {
			defer group.Done()
			calls[i].response, calls[i].err = broker.Dispatch(context.Background(), raw, fixtureIdentity())
		}(i, broker)
	}
	group.Wait()
	return calls
}
func TestConcurrentSubmissionsAcrossJournalConnections(t *testing.T) {
	f := newTaskFixture(t)
	calls := concurrentSubmissions(f.independentBrokers(8), f.submission(f.approve()))
	f.assertOneAcceptance(calls)
	f.taskCounts(1, 1, 1)
	f.auditReceipts(9)
}
func (f *taskFixture) independentBrokers(count int) []*Broker {
	brokers := make([]*Broker, count)
	for i := range brokers {
		brokers[i] = f.independentBroker()
	}
	return brokers
}
func (f *taskFixture) independentBroker() *Broker {
	journal, err := OpenJournal(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = journal.Close() })
	return testBroker(f.t, journal, f.config)
}
func (f *taskFixture) assertOneAcceptance(calls []taskCall) {
	original := calls[0].assertion(f.t).run()
	accepted := 0
	for _, call := range calls {
		result := call.assertion(f.t)
		result.sameRun(original)
		if !result.response.Replayed {
			accepted++
		}
		f.persisted(result.response.Receipt)
	}
	if accepted != 1 {
		f.t.Fatal("expected one acceptance", accepted)
	}
}
func (call taskCall) assertion(t *testing.T) taskAssertion {
	if call.err != nil {
		t.Fatal(call.err)
	}
	return taskAssertion{t: t, response: call.response}
}
