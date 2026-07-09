package quality

import "hash"

// fnv128 aliases the 128-bit hash used for row and value identity.
type fnv128 = hash.Hash

// rowKey hashes a full record (fields joined by a unit separator).
func rowKey(rec []string, h fnv128) [16]byte {
	h.Reset()
	for i, v := range rec {
		if i > 0 {
			h.Write([]byte{0x1f})
		}
		h.Write([]byte(v))
	}
	var key [16]byte
	h.Sum(key[:0])
	return key
}

// valueKey hashes a single value.
func valueKey(v string, h fnv128) [16]byte {
	h.Reset()
	h.Write([]byte(v))
	var key [16]byte
	h.Sum(key[:0])
	return key
}
