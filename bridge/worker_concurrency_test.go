package bridge

import (
	"context"
	"sync"
	"testing"
)

type workerPool struct {
	workers []*Worker
	calls   []taskCall
}

func (f *stubFixture) independentWorker() *Worker {
	journal, err := OpenJournal(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = journal.Close() })
	adapter, err := NewStubAdapter(journal, f.scenario)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.workerFor(journal, adapter)
}
func (f *stubFixture) workerFor(journal *Journal, adapter ExecutionAdapter) *Worker {
	worker, err := f.brokerFor(journal).NewWorker(adapter)
	if err != nil {
		f.t.Fatal(err)
	}
	return worker
}
func (f *stubFixture) workerPool() *workerPool {
	pool := &workerPool{calls: make([]taskCall, 8)}
	for i := 0; i < 8; i++ {
		pool.workers = append(pool.workers, f.independentWorker())
	}
	return pool
}
func (p *workerPool) run() {
	var group sync.WaitGroup
	for i := range p.workers {
		group.Add(1)
		go func(i int) { defer group.Done(); p.tick(i) }(i)
	}
	group.Wait()
}
func (p *workerPool) tick(i int) {
	p.calls[i].response, p.calls[i].err = p.workers[i].Tick(context.Background())
}
func (p *workerPool) audited(f *stubFixture) {
	for _, call := range p.calls {
		f.auditCall(call)
	}
}
func TestConcurrentWorkersAcrossJournalConnectionsStartOnce(t *testing.T) {
	f := newStubFixture(t, "success")
	f.queued()
	pool := f.workerPool()
	pool.run()
	pool.audited(f)
	f.starts(1)
}

func (f *stubFixture) brokerFor(journal *Journal) *Broker { return testBroker(f.t, journal, f.config) }
