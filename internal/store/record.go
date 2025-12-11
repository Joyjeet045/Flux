package store

import (
	"encoding/binary"
	"nats-lite/internal/headers"
)

type Record struct {
	Sequence  uint64
	Timestamp int64
	Subject   string
	Headers   headers.Headers
	Data      []byte
}

// Encode serializes the record:
// [TotalLen(4)] [Sequence(8)] [Timestamp(8)] [SubjectLen(2)] [Subject] [HeadersLen(4)] [Headers] [Data]
func (r *Record) Encode() []byte {
	subjBytes := []byte(r.Subject)
	headerBytes := []byte{}
	if r.Headers != nil {
		headerBytes = r.Headers.Encode()
	}

	totalLen := 8 + 8 + 2 + len(subjBytes) + 4 + len(headerBytes) + len(r.Data)

	buf := make([]byte, 4+totalLen)

	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLen))
	binary.BigEndian.PutUint64(buf[4:12], r.Sequence)
	binary.BigEndian.PutUint64(buf[12:20], uint64(r.Timestamp))
	binary.BigEndian.PutUint16(buf[20:22], uint16(len(subjBytes)))
	copy(buf[22:], subjBytes)

	offset := 22 + len(subjBytes)
	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(headerBytes)))
	offset += 4
	copy(buf[offset:], headerBytes)
	offset += len(headerBytes)
	copy(buf[offset:], r.Data)

	return buf
}

func Decode(data []byte) (*Record, error) {
	if len(data) < 4+8+8+2 {
		return nil, nil
	}

	seq := binary.BigEndian.Uint64(data[4:12])
	ts := int64(binary.BigEndian.Uint64(data[12:20]))
	subjLen := binary.BigEndian.Uint16(data[20:22])

	if len(data) < 22+int(subjLen)+4 {
		return nil, nil
	}

	subject := string(data[22 : 22+subjLen])
	offset := 22 + int(subjLen)

	headersLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	var hdrs headers.Headers
	if headersLen > 0 {
		if len(data) < offset+int(headersLen) {
			return nil, nil
		}
		var err error
		hdrs, err = headers.Decode(data[offset : offset+int(headersLen)])
		if err != nil {
			return nil, err
		}
		offset += int(headersLen)
	}

	payload := make([]byte, len(data)-offset)
	copy(payload, data[offset:])

	return &Record{
		Sequence:  seq,
		Timestamp: ts,
		Subject:   subject,
		Headers:   hdrs,
		Data:      payload,
	}, nil
}
