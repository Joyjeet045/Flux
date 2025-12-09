package store

import (
	"encoding/binary"
	"os"
)

type Segment struct {
	BaseSequence uint64
	Path         string
	file         *os.File
	currentSize  int64
	nextOffset   int64 // Local offset in file
}

func NewSegment(path string, baseSeq uint64) (*Segment, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Segment{
		BaseSequence: baseSeq,
		Path:         path,
		file:         f,
		currentSize:  info.Size(),
		nextOffset:   info.Size(),
	}, nil
}

func (s *Segment) Append(record *Record) (int64, int, error) {
	bytes := record.Encode()
	n, err := s.file.Write(bytes)
	if err != nil {
		return 0, 0, err
	}

	startOffset := s.nextOffset

	s.nextOffset += int64(n)
	s.currentSize += int64(n)

	return startOffset, n, nil
}

func (s *Segment) ReadAt(offset int64) (*Record, int64, error) {
	// Read Length (4 bytes)
	header := make([]byte, 4)
	_, err := s.file.ReadAt(header, offset)
	if err != nil {
		return nil, 0, err
	}
	totalLen := binary.BigEndian.Uint32(header)

	// Read Record
	recordBuf := make([]byte, 4+totalLen) // Include Header in buffer for Decode consistency
	_, err = s.file.ReadAt(recordBuf, offset)
	if err != nil {
		return nil, 0, err
	}

	rec, err := Decode(recordBuf)
	if err != nil {
		return nil, 0, err
	}

	return rec, int64(4 + totalLen), nil
}

func (s *Segment) Close() error {
	return s.file.Close()
}

func (s *Segment) Remove() error {
	s.Close()
	return os.Remove(s.Path)
}
