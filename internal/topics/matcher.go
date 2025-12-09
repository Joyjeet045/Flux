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
	Queue   string
	Client  Subscriber
}

// TrieNode represents a node in the subscription trie
type TrieNode struct {
	children      map[string]*TrieNode
	subscriptions []*Subscription
	wildcardSubs  []*Subscription // * subscriptions at this level
	catchAllSubs  []*Subscription // > subscriptions at this level
}

func newTrieNode() *TrieNode {
	return &TrieNode{
		children:      make(map[string]*TrieNode),
		subscriptions: make([]*Subscription, 0),
		wildcardSubs:  make([]*Subscription, 0),
		catchAllSubs:  make([]*Subscription, 0),
	}
}

type Matcher struct {
	mu       sync.RWMutex
	root     *TrieNode
	balancer *QueueGroupBalancer
}

func NewMatcher() *Matcher {
	return &Matcher{
		root:     newTrieNode(),
		balancer: NewQueueGroupBalancer(),
	}
}

type QueueGroupBalancer struct {
	mu       sync.Mutex
	counters map[string]int
}

func NewQueueGroupBalancer() *QueueGroupBalancer {
	return &QueueGroupBalancer{
		counters: make(map[string]int),
	}
}

func (qgb *QueueGroupBalancer) SelectMember(queueName string, memberCount int) int {
	qgb.mu.Lock()
	defer qgb.mu.Unlock()

	idx := qgb.counters[queueName] % memberCount
	qgb.counters[queueName]++

	return idx
}

func (m *Matcher) Subscribe(sub *Subscription) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tokens := strings.Split(sub.Subject, ".")
	m.insertSub(m.root, tokens, 0, sub)
}

func (m *Matcher) insertSub(node *TrieNode, tokens []string, idx int, sub *Subscription) {
	if idx >= len(tokens) {
		node.subscriptions = append(node.subscriptions, sub)
		return
	}

	token := tokens[idx]

	if token == ">" {
		// Catch-all wildcard - matches everything from here
		node.catchAllSubs = append(node.catchAllSubs, sub)
		return
	}

	if token == "*" {
		// Single-level wildcard
		node.wildcardSubs = append(node.wildcardSubs, sub)
		// Continue to next level for proper matching
		if idx+1 < len(tokens) {
			// Create a special wildcard path
			if node.children["*"] == nil {
				node.children["*"] = newTrieNode()
			}
			m.insertSub(node.children["*"], tokens, idx+1, sub)
		}
		return
	}

	// Regular token
	if node.children[token] == nil {
		node.children[token] = newTrieNode()
	}
	m.insertSub(node.children[token], tokens, idx+1, sub)
}

func (m *Matcher) Match(subject string) []*Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tokens := strings.Split(subject, ".")
	matches := make([]*Subscription, 0)
	visited := make(map[*Subscription]bool)

	m.matchRecursive(m.root, tokens, 0, &matches, visited)

	// Apply queue group logic
	return m.applyQueueGroupLogic(matches)
}

func (m *Matcher) matchRecursive(node *TrieNode, tokens []string, idx int, matches *[]*Subscription, visited map[*Subscription]bool) {
	// Add catch-all subscriptions (>) at any level
	for _, sub := range node.catchAllSubs {
		if !visited[sub] {
			*matches = append(*matches, sub)
			visited[sub] = true
		}
	}

	// If we've consumed all tokens, add exact matches
	if idx >= len(tokens) {
		for _, sub := range node.subscriptions {
			if !visited[sub] {
				*matches = append(*matches, sub)
				visited[sub] = true
			}
		}
		return
	}

	token := tokens[idx]

	// 1. Try exact match
	if child, ok := node.children[token]; ok {
		m.matchRecursive(child, tokens, idx+1, matches, visited)
	}

	// 2. Try wildcard (*) match
	if child, ok := node.children["*"]; ok {
		m.matchRecursive(child, tokens, idx+1, matches, visited)
	}

	// 3. Check single-level wildcards at this node
	if idx+1 >= len(tokens) {
		// Last token - wildcards here match
		for _, sub := range node.wildcardSubs {
			if !visited[sub] {
				*matches = append(*matches, sub)
				visited[sub] = true
			}
		}
	}
}

func (m *Matcher) applyQueueGroupLogic(matches []*Subscription) []*Subscription {
	if len(matches) == 0 {
		return matches
	}

	// Group subscriptions by queue name
	queueGroups := make(map[string][]*Subscription)
	regularSubs := make([]*Subscription, 0)

	for _, sub := range matches {
		if sub.Queue != "" {
			queueGroups[sub.Queue] = append(queueGroups[sub.Queue], sub)
		} else {
			regularSubs = append(regularSubs, sub)
		}
	}

	result := make([]*Subscription, 0, len(regularSubs)+len(queueGroups))
	result = append(result, regularSubs...)

	// For each queue group, select one member using round-robin
	for queueName, members := range queueGroups {
		if len(members) > 0 {
			idx := m.balancer.SelectMember(queueName, len(members))
			result = append(result, members[idx])
		}
	}

	return result
}
