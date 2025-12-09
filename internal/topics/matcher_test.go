package topics

import (
	"fmt"
	"testing"
	"time"
)

type mockClient struct{}

func (m *mockClient) Send(sid string, subject string, payload []byte, seq uint64) {}

func BenchmarkMatcherExact(b *testing.B) {
	m := NewMatcher()
	client := &mockClient{}

	// Add 1000 exact subscriptions
	for i := 0; i < 1000; i++ {
		m.Subscribe(&Subscription{
			Subject: fmt.Sprintf("topic.%d", i),
			Sid:     fmt.Sprintf("sid-%d", i),
			Client:  client,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match("topic.500")
	}
}

func BenchmarkMatcherWildcard(b *testing.B) {
	m := NewMatcher()
	client := &mockClient{}

	// Add 1000 wildcard subscriptions
	for i := 0; i < 1000; i++ {
		m.Subscribe(&Subscription{
			Subject: fmt.Sprintf("topic.*.sub%d", i),
			Sid:     fmt.Sprintf("sid-%d", i),
			Client:  client,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match("topic.test.sub500")
	}
}

func BenchmarkMatcherCatchAll(b *testing.B) {
	m := NewMatcher()
	client := &mockClient{}

	// Add 100 catch-all subscriptions
	for i := 0; i < 100; i++ {
		m.Subscribe(&Subscription{
			Subject: fmt.Sprintf("topic.%d.>", i),
			Sid:     fmt.Sprintf("sid-%d", i),
			Client:  client,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match("topic.50.a.b.c.d.e")
	}
}

func TestMatcherCorrectness(t *testing.T) {
	m := NewMatcher()
	client := &mockClient{}

	// Test exact match
	m.Subscribe(&Subscription{Subject: "orders.us.created", Sid: "1", Client: client})
	matches := m.Match("orders.us.created")
	if len(matches) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matches))
	}

	// Test wildcard *
	m.Subscribe(&Subscription{Subject: "orders.*.created", Sid: "2", Client: client})
	matches = m.Match("orders.eu.created")
	if len(matches) != 1 {
		t.Errorf("Expected 1 match for wildcard, got %d", len(matches))
	}

	// Test catch-all >
	m.Subscribe(&Subscription{Subject: "logs.>", Sid: "3", Client: client})
	matches = m.Match("logs.error.server.critical")
	if len(matches) != 1 {
		t.Errorf("Expected 1 match for catch-all, got %d", len(matches))
	}

	// Test multiple matches
	matches = m.Match("orders.us.created")
	if len(matches) != 2 { // Exact + wildcard
		t.Errorf("Expected 2 matches, got %d", len(matches))
	}
}

func TestQueueGroupLoadBalancing(t *testing.T) {
	m := NewMatcher()
	client := &mockClient{}

	// Add 3 workers to same queue group
	m.Subscribe(&Subscription{Subject: "jobs", Sid: "w1", Queue: "workers", Client: client})
	m.Subscribe(&Subscription{Subject: "jobs", Sid: "w2", Queue: "workers", Client: client})
	m.Subscribe(&Subscription{Subject: "jobs", Sid: "w3", Queue: "workers", Client: client})

	// Should only get 1 match (one worker from the group)
	matches := m.Match("jobs")
	if len(matches) != 1 {
		t.Errorf("Expected 1 match (queue group), got %d", len(matches))
	}
}

func TestPerformanceComparison(t *testing.T) {
	m := NewMatcher()
	client := &mockClient{}

	// Add 10,000 subscriptions
	for i := 0; i < 10000; i++ {
		m.Subscribe(&Subscription{
			Subject: fmt.Sprintf("topic.%d.events", i),
			Sid:     fmt.Sprintf("sid-%d", i),
			Client:  client,
		})
	}

	// Measure match time
	start := time.Now()
	for i := 0; i < 1000; i++ {
		m.Match("topic.5000.events")
	}
	elapsed := time.Since(start)

	avgTime := elapsed / 1000
	t.Logf("Average match time with 10K subs: %v", avgTime)

	// Should be sub-millisecond
	if avgTime > time.Millisecond {
		t.Errorf("Match too slow: %v (expected < 1ms)", avgTime)
	}
}
