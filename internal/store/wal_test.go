package store

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

func TestNewStore(t *testing.T) {
	dir, err := ioutil.TempDir("", "nats-store-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := NewStore(dir, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	if store.SegmentCount() != 1 {
		t.Errorf("Expected 1 segment, got %d", store.SegmentCount())
	}
}

func TestAppendAndRead(t *testing.T) {
	dir, err := ioutil.TempDir("", "nats-store-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := NewStore(dir, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	subject := "test.subject"
	data := []byte("hello world")

	seq, err := store.Append(subject, data)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	if seq != 1 {
		t.Errorf("Expected seq 1, got %d", seq)
	}

	rec, err := store.Read(seq)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if rec.Subject != subject {
		t.Errorf("Expected subject %s, got %s", subject, rec.Subject)
	}
	if !bytes.Equal(rec.Data, data) {
		t.Errorf("Expected data %s, got %s", data, rec.Data)
	}
}

func TestSegmentRotation(t *testing.T) {
	dir, err := ioutil.TempDir("", "nats-store-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Small segment size to force rotation
	maxSize := int64(100)
	store, err := NewStore(dir, maxSize)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// data size: ~10 (header) + 5 (subject) + 10 (payload) = ~25 bytes per record
	// writing 5 records should strictly exceed 100 bytes and cause rotation

	for i := 0; i < 5; i++ {
		_, err := store.Append("test", []byte("0123456789"))
		if err != nil {
			t.Fatalf("Append failed at %d: %v", i, err)
		}
	}

	if store.SegmentCount() < 2 {
		t.Errorf("Expected at least 2 segments, got %d", store.SegmentCount())
	}

	// Read all back
	for i := 1; i <= 5; i++ {
		rec, err := store.Read(uint64(i))
		if err != nil {
			t.Errorf("Failed to read seq %d: %v", i, err)
			continue
		}
		if string(rec.Data) != "0123456789" {
			t.Errorf("Data mismatch at seq %d", i)
		}
	}
}

func TestRecovery(t *testing.T) {
	dir, err := ioutil.TempDir("", "nats-store-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Open, write, close
	func() {
		store, err := NewStore(dir, 1024*1024)
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}
		defer store.Close()

		for i := 0; i < 10; i++ {
			store.Append("test", []byte(fmt.Sprintf("msg-%d", i)))
		}
	}()

	// Reopen
	store, err := NewStore(dir, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	if store.nextSeq != 11 {
		t.Errorf("Expected nextSeq 11, got %d", store.nextSeq)
	}

	// Read last msg
	rec, err := store.Read(10)
	if err != nil {
		t.Fatalf("Failed to read recovered msg 10: %v", err)
	}
	if string(rec.Data) != "msg-9" {
		t.Errorf("Expected msg-9, got %s", string(rec.Data))
	}
}
