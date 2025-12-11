package headers

import (
	"bytes"
	"fmt"
	"strings"
)

const (
	MessageID     = "Msg-ID"
	Timestamp     = "Timestamp"
	TraceID       = "Trace-ID"
	UserID        = "User-ID"
	CorrelationID = "Correlation-ID"
	ContentType   = "Content-Type"
	Priority      = "Priority"
	ReplyTo       = "Reply-To"
)

type Headers map[string]string

func New() Headers {
	return make(Headers)
}

func (h Headers) Set(key, value string) {
	h[key] = value
}

func (h Headers) Get(key string) string {
	return h[key]
}

func (h Headers) Has(key string) bool {
	_, ok := h[key]
	return ok
}

func (h Headers) Delete(key string) {
	delete(h, key)
}

func (h Headers) Clone() Headers {
	clone := make(Headers, len(h))
	for k, v := range h {
		clone[k] = v
	}
	return clone
}

func (h Headers) Encode() []byte {
	if len(h) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for key, value := range h {
		buf.WriteString(key)
		buf.WriteString(": ")
		buf.WriteString(value)
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")

	return buf.Bytes()
}

func Decode(data []byte) (Headers, error) {
	if len(data) == 0 {
		return nil, nil
	}

	h := New()
	lines := bytes.Split(data, []byte("\r\n"))

	for _, line := range lines {
		if len(line) == 0 {
			break
		}

		parts := bytes.SplitN(line, []byte(": "), 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header format: %s", string(line))
		}

		key := strings.TrimSpace(string(parts[0]))
		value := strings.TrimSpace(string(parts[1]))
		h.Set(key, value)
	}

	return h, nil
}

func (h Headers) String() string {
	if len(h) == 0 {
		return "{}"
	}

	var buf bytes.Buffer
	buf.WriteString("{")
	first := true
	for k, v := range h {
		if !first {
			buf.WriteString(", ")
		}
		buf.WriteString(k)
		buf.WriteString(": ")
		buf.WriteString(v)
		first = false
	}
	buf.WriteString("}")
	return buf.String()
}
