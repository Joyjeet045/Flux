package dedup

import (
	"testing"
	"time"
)

func TestDeduplicatorBasic(t *testing.T) {
	d := New(1*time.Second, 100)
	defer d.Close()

	msgID := "test-message-1"

	if d.IsDuplicate(msgID) {
		t.Error("First occurrence should not be duplicate")
	}

	if !d.IsDuplicate(msgID) {
		t.Error("Second occurrence should be duplicate")
	}

	stats := d.Stats()
	if stats.TotalChecked != 2 {
		t.Errorf("Expected 2 checks, got %d", stats.TotalChecked)
	}

	if stats.DuplicatesFound != 1 {
		t.Errorf("Expected 1 duplicate, got %d", stats.DuplicatesFound)
	}

	if stats.UniqueMessages != 1 {
		t.Errorf("Expected 1 unique message, got %d", stats.UniqueMessages)
	}
}

func TestDeduplicatorExpiration(t *testing.T) {
	d := New(100*time.Millisecond, 100)
	defer d.Close()

	msgID := "test-message-expire"

	if d.IsDuplicate(msgID) {
		t.Error("First occurrence should not be duplicate")
	}

	time.Sleep(150 * time.Millisecond)

	if d.IsDuplicate(msgID) {
		t.Error("Message should have expired and not be duplicate")
	}

	stats := d.Stats()
	if stats.UniqueMessages != 2 {
		t.Errorf("Expected 2 unique messages (after expiration), got %d", stats.UniqueMessages)
	}
}

func TestDeduplicatorMaxEntries(t *testing.T) {
	maxEntries := 10
	d := New(10*time.Second, maxEntries)
	defer d.Close()

	for i := 0; i < maxEntries+5; i++ {
		msgID := string(rune('a' + i))
		d.IsDuplicate(msgID)
	}

	stats := d.Stats()
	if stats.WindowSize > maxEntries {
		t.Errorf("Window size %d exceeds max entries %d", stats.WindowSize, maxEntries)
	}
}

func TestDeduplicatorGenerateID(t *testing.T) {
	d := New(1*time.Second, 100)
	defer d.Close()

	id1 := d.GenerateID("subject1", []byte("payload1"))
	id2 := d.GenerateID("subject1", []byte("payload1"))
	id3 := d.GenerateID("subject2", []byte("payload1"))

	if id1 == id2 {
		t.Error("Generated IDs should be unique (include timestamp)")
	}

	if id1 == id3 {
		t.Error("Different subjects should generate different IDs")
	}

	if len(id1) != 32 {
		t.Errorf("Expected ID length 32, got %d", len(id1))
	}
}

func TestDeduplicatorEmptyMessageID(t *testing.T) {
	d := New(1*time.Second, 100)
	defer d.Close()

	if d.IsDuplicate("") {
		t.Error("Empty message ID should not be considered duplicate")
	}

	if d.IsDuplicate("") {
		t.Error("Empty message ID should never be duplicate")
	}

	stats := d.Stats()
	if stats.DuplicatesFound != 0 {
		t.Errorf("Expected 0 duplicates for empty IDs, got %d", stats.DuplicatesFound)
	}
}

func TestDeduplicatorReset(t *testing.T) {
	d := New(1*time.Second, 100)
	defer d.Close()

	d.IsDuplicate("msg1")
	d.IsDuplicate("msg2")
	d.IsDuplicate("msg1")

	stats := d.Stats()
	if stats.TotalChecked != 3 {
		t.Errorf("Expected 3 checks before reset, got %d", stats.TotalChecked)
	}

	d.Reset()

	stats = d.Stats()
	if stats.TotalChecked != 0 {
		t.Errorf("Expected 0 checks after reset, got %d", stats.TotalChecked)
	}

	if stats.WindowSize != 0 {
		t.Errorf("Expected window size 0 after reset, got %d", stats.WindowSize)
	}
}

func TestDeduplicatorConcurrent(t *testing.T) {
	d := New(1*time.Second, 1000)
	defer d.Close()

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				msgID := string(rune('a' + (id*100+j)%26))
				d.IsDuplicate(msgID)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	stats := d.Stats()
	if stats.TotalChecked != 1000 {
		t.Errorf("Expected 1000 checks, got %d", stats.TotalChecked)
	}
}
