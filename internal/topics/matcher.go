package topics

import (
	"strings"
	"sync"
)

type Subscriber interface {
	Send(sid string, subject string, payload []byte, seq uint64)
}

type Subscription struct {
	Subject string
	Sid     string
	Queue   string // Empty if standard subscription
	Client  Subscriber
}

type Matcher struct {
	mu   sync.RWMutex
	subs map[string][]*Subscription // Simple map for exact match basic, will upgrade to Trie
	// For MVP, we can iterate for wildcards or use a better structure.
	// Let's do a simple list for wildcard support to ensure correctness first.
	wildcardSubs []*Subscription
}

func NewMatcher() *Matcher {
	return &Matcher{
		subs:         make(map[string][]*Subscription),
		wildcardSubs: make([]*Subscription, 0),
	}
}

func (m *Matcher) Subscribe(sub *Subscription) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.Contains(sub.Subject, "*") || strings.Contains(sub.Subject, ">") {
		m.wildcardSubs = append(m.wildcardSubs, sub)
	} else {
		m.subs[sub.Subject] = append(m.subs[sub.Subject], sub)
	}
}

func (m *Matcher) Match(subject string) []*Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matches []*Subscription

	// Group matching logic
	// Map to track if a queue group has already been selected for this message
	groupSelected := make(map[string]bool)

	// Helper to add match respecting queue groups
	addMatch := func(sub *Subscription) {
		if sub.Queue != "" {
			if groupSelected[sub.Queue] {
				return // Already selected a member for this group
			}
			// Only pick ONE member of the group
			// For MVP: First one wins (simplified load balancing).
			// Real LB requires randomization or round-robin state.
			// Let's implement Random selection later, for now: First Win.
			// Actually, let's do simple Round Robin if we had the list, but here we are iterating.
			// Since we iterate, "first one" is deterministic.
			// To be fair, we should shuffle or rotate.
			// For basic functional requirement: "Going to only one worker" -> CHECK.
			groupSelected[sub.Queue] = true
			matches = append(matches, sub)
		} else {
			matches = append(matches, sub)
		}
	}

	// Exact matches
	if s, ok := m.subs[subject]; ok {
		for _, sub := range s {
			addMatch(sub)
		}
	}

	// Wildcard matches
	tokens := strings.Split(subject, ".")
	for _, sub := range m.wildcardSubs {
		if matchOne(sub.Subject, tokens) {
			addMatch(sub)
		}
	}

	return matches
}

func matchOne(pattern string, subjectTokens []string) bool {
	patternTokens := strings.Split(pattern, ".")

	pIdx, sIdx := 0, 0
	for pIdx < len(patternTokens) && sIdx < len(subjectTokens) {
		token := patternTokens[pIdx]
		if token == ">" {
			return true
		}
		if token == "*" || token == subjectTokens[sIdx] {
			pIdx++
			sIdx++
			continue
		}
		return false
	}
	return pIdx == len(patternTokens) && sIdx == len(subjectTokens)
}
