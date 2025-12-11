package headers

import (
	"testing"
)

func TestHeadersBasic(t *testing.T) {
	h := New()

	h.Set("Msg-ID", "12345")
	h.Set("Trace-ID", "trace-abc")

	if h.Get("Msg-ID") != "12345" {
		t.Errorf("Expected Msg-ID to be 12345, got %s", h.Get("Msg-ID"))
	}

	if !h.Has("Trace-ID") {
		t.Error("Expected Trace-ID to exist")
	}

	h.Delete("Trace-ID")
	if h.Has("Trace-ID") {
		t.Error("Expected Trace-ID to be deleted")
	}
}

func TestHeadersEncodeDecode(t *testing.T) {
	h := New()
	h.Set(MessageID, "msg-123")
	h.Set(TraceID, "trace-456")
	h.Set(UserID, "user-789")

	encoded := h.Encode()
	if len(encoded) == 0 {
		t.Fatal("Encoded headers should not be empty")
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Failed to decode headers: %v", err)
	}

	if decoded.Get(MessageID) != "msg-123" {
		t.Errorf("Expected MessageID msg-123, got %s", decoded.Get(MessageID))
	}

	if decoded.Get(TraceID) != "trace-456" {
		t.Errorf("Expected TraceID trace-456, got %s", decoded.Get(TraceID))
	}

	if decoded.Get(UserID) != "user-789" {
		t.Errorf("Expected UserID user-789, got %s", decoded.Get(UserID))
	}
}

func TestHeadersClone(t *testing.T) {
	h := New()
	h.Set("key1", "value1")
	h.Set("key2", "value2")

	clone := h.Clone()
	clone.Set("key3", "value3")

	if h.Has("key3") {
		t.Error("Original headers should not have key3")
	}

	if !clone.Has("key1") {
		t.Error("Cloned headers should have key1")
	}
}

func TestHeadersEmpty(t *testing.T) {
	h := New()
	encoded := h.Encode()

	if encoded != nil {
		t.Error("Empty headers should encode to nil")
	}

	decoded, err := Decode(nil)
	if err != nil {
		t.Fatalf("Failed to decode nil headers: %v", err)
	}

	if decoded != nil {
		t.Error("Decoded nil should be nil")
	}
}
