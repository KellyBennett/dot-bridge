package bridge

import (
	"context"
	"sort"
	"sync"
	"testing"
)

type concurrentRead struct {
	response Response
	err      error
}
type readPool struct {
	fixtures []*bridgeFixture
	results  chan concurrentRead
}

func (f *bridgeFixture) sibling() *bridgeFixture {
	sibling := &bridgeFixture{t: f.t, path: f.path, config: fixtureConfig()}
	sibling.open()
	sibling.rebuild()
	f.t.Cleanup(sibling.close)
	return sibling
}

func newReadPool(f *bridgeFixture) *readPool {
	return &readPool{fixtures: []*bridgeFixture{f, f.sibling(), f.sibling(), f.sibling()}, results: make(chan concurrentRead, 12)}
}

func (p *readPool) worker(f *bridgeFixture) {
	r, err := f.attempt(context.Background())
	p.results <- concurrentRead{response: r, err: err}
}

func (p *readPool) run() {
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(f *bridgeFixture) {
			defer wg.Done()
			p.worker(f)
		}(p.fixtures[i%len(p.fixtures)])
	}
	wg.Wait()
	close(p.results)
}

func (p *readPool) sequences(t *testing.T) []int {
	var sequences []int
	for result := range p.results {
		if result.err != nil {
			t.Fatal("concurrent read failed", result.err)
		}
		responseAssertion{t: t, response: result.response}.allowed()
		sequences = append(sequences, int(result.response.Receipt.JournalSequence))
	}
	return sequences
}

func orderedSequences(t *testing.T, sequences []int) {
	sort.Ints(sequences)
	if len(sequences) != 12 {
		t.Fatal("concurrent result count differs")
	}
	for i, sequence := range sequences {
		if sequence != i+1 {
			t.Fatal("duplicate or missing journal sequence", sequences)
		}
	}
}

func TestConcurrentConnectionsCommitUniqueReceipts(t *testing.T) {
	f := newFixture(t)
	pool := newReadPool(f)
	pool.run()
	orderedSequences(t, pool.sequences(t))
	f.probe().count(12)
}
