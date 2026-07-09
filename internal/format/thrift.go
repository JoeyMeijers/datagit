package format

// Minimal Apache Thrift *compact protocol* decoder, sufficient to read
// the Parquet footer (FileMetaData). Implemented in-house so dit can
// extract Parquet metadata without importing a Parquet/Thrift dependency
// tree; see ADR-0003. Only decoding is supported, and only the subset of
// the protocol Parquet uses.
//
// Reference: thrift compact protocol spec
// https://github.com/apache/thrift/blob/master/doc/specs/thrift-compact-protocol.md

import (
	"errors"
	"fmt"
)

// Compact protocol type IDs.
const (
	tStop      = 0
	tBoolTrue  = 1
	tBoolFalse = 2
	tByte      = 3
	tI16       = 4
	tI32       = 5
	tI64       = 6
	tDouble    = 7
	tBinary    = 8
	tList      = 9
	tSet       = 10
	tMap       = 11
	tStruct    = 12
)

var errTruncated = errors.New("truncated thrift data")

// thriftReader decodes compact-protocol values from a byte slice.
type thriftReader struct {
	buf []byte
	pos int
}

func (r *thriftReader) byte() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, errTruncated
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

// uvarint reads a ULEB128-encoded unsigned integer.
func (r *thriftReader) uvarint() (uint64, error) {
	var v uint64
	for shift := 0; shift < 64; shift += 7 {
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("thrift varint overflow")
}

// varint reads a zigzag-encoded signed integer.
func (r *thriftReader) varint() (int64, error) {
	u, err := r.uvarint()
	if err != nil {
		return 0, err
	}
	return int64(u>>1) ^ -int64(u&1), nil
}

// binary reads a length-prefixed byte string.
func (r *thriftReader) binary() ([]byte, error) {
	n, err := r.uvarint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.buf)-r.pos) {
		return nil, errTruncated
	}
	b := r.buf[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return b, nil
}

// fieldHeader reads the next struct field header. done is true at STOP.
// lastID is the previous field ID (compact protocol delta-encodes IDs).
func (r *thriftReader) fieldHeader(lastID int16) (id int16, typ byte, done bool, err error) {
	b, err := r.byte()
	if err != nil {
		return 0, 0, false, err
	}
	if b == tStop {
		return 0, 0, true, nil
	}
	typ = b & 0x0f
	delta := int16(b >> 4)
	if delta == 0 {
		v, err := r.varint()
		if err != nil {
			return 0, 0, false, err
		}
		id = int16(v)
	} else {
		id = lastID + delta
	}
	return id, typ, false, nil
}

// listHeader reads a list/set header: element type and count.
func (r *thriftReader) listHeader() (elemType byte, size int, err error) {
	b, err := r.byte()
	if err != nil {
		return 0, 0, err
	}
	elemType = b & 0x0f
	size = int(b >> 4)
	if size == 15 {
		n, err := r.uvarint()
		if err != nil {
			return 0, 0, err
		}
		size = int(n)
	}
	if size < 0 || size > len(r.buf)-r.pos {
		return 0, 0, fmt.Errorf("thrift list size %d exceeds buffer", size)
	}
	return elemType, size, nil
}

// skip discards a value of the given compact type, recursing as needed.
func (r *thriftReader) skip(typ byte) error {
	switch typ {
	case tBoolTrue, tBoolFalse:
		return nil // value encoded in the type nibble
	case tByte:
		_, err := r.byte()
		return err
	case tI16, tI32, tI64:
		_, err := r.varint()
		return err
	case tDouble:
		if len(r.buf)-r.pos < 8 {
			return errTruncated
		}
		r.pos += 8
		return nil
	case tBinary:
		_, err := r.binary()
		return err
	case tList, tSet:
		elemType, size, err := r.listHeader()
		if err != nil {
			return err
		}
		for i := 0; i < size; i++ {
			if err := r.skipListElem(elemType); err != nil {
				return err
			}
		}
		return nil
	case tMap:
		n, err := r.uvarint()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		kv, err := r.byte()
		if err != nil {
			return err
		}
		for i := uint64(0); i < n; i++ {
			if err := r.skipListElem(kv >> 4); err != nil {
				return err
			}
			if err := r.skipListElem(kv & 0x0f); err != nil {
				return err
			}
		}
		return nil
	case tStruct:
		var last int16
		for {
			id, ft, done, err := r.fieldHeader(last)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			if err := r.skip(ft); err != nil {
				return err
			}
			last = id
		}
	default:
		return fmt.Errorf("unknown thrift compact type %d", typ)
	}
}

// skipListElem skips one list/set/map element. Unlike struct fields,
// booleans inside collections occupy one byte.
func (r *thriftReader) skipListElem(typ byte) error {
	if typ == tBoolTrue || typ == tBoolFalse {
		_, err := r.byte()
		return err
	}
	return r.skip(typ)
}
