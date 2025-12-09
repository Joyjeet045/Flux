package store

import (
	"encoding/binary"
)

type Record struct {
	Sequence  uint64
	Timestamp int64
	Subject   string
	Data      []byte
}

// Encode serializes the record:
// [TotalLen(4)] [Sequence(8)] [Timestamp(8)] [SubjectLen(2)] [Subject] [Data]
func (r *Record) Encode() []byte {
	subjBytes := []byte(r.Subject)
	totalLen := 8 + 8 + 2 + len(subjBytes) + len(r.Data)

	buf := make([]byte, 4+totalLen)

	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLen))
	binary.BigEndian.PutUint64(buf[4:12], r.Sequence)
	binary.BigEndian.PutUint64(buf[12:20], uint64(r.Timestamp))
	binary.BigEndian.PutUint16(buf[20:22], uint16(len(subjBytes)))
	copy(buf[22:], subjBytes)
	copy(buf[22+len(subjBytes):], r.Data)

	return buf
}

func Decode(data []byte) (*Record, error) {
	// Assumes data includes the totalLen header or we skip it before calling?
	// Let's assume passed slice STARTS after the length prefix for simpler logic,
	// or we pass the full block.
	// Let's assume we pass the full block including length for safety.

	if len(data) < 4+8+8+2 {
		return nil, nil // Not enough data
	}

	// size := binary.BigEndian.Uint32(data[0:4])
	seq := binary.BigEndian.Uint64(data[4:12])
	ts := int64(binary.BigEndian.Uint64(data[12:20]))
	subjLen := binary.BigEndian.Uint16(data[20:22])

	if len(data) < 22+int(subjLen) {
		return nil, nil
	}

	subject := string(data[22 : 22+subjLen])
	payload := make([]byte, len(data)-(22+int(subjLen)))
	copy(payload, data[22+subjLen:])

	return &Record{
		Sequence:  seq,
		Timestamp: ts,
		Subject:   subject,
		Data:      payload,
	}, nil
}
