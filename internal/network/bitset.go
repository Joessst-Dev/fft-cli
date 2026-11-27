package network

import "math/bits"

// Bitset is a set of facility indices. A production network has tens of
// thousands of facilities and well over a hundred thousand connections, and the
// resolver intersects a target set with a context's set for every connection and
// every order profile; a map per set would not fit, and would not be fast.
//
// A nil Bitset is the empty set for every read.
type Bitset []uint64

// NewBitset is an empty set able to hold the indices 0..n-1.
func NewBitset(n int) Bitset { return make(Bitset, (n+63)/64) }

// Set adds i.
func (b Bitset) Set(i int) { b[i/64] |= 1 << (uint(i) % 64) }

// Has reports whether i is in the set.
func (b Bitset) Has(i int) bool {
	w := i / 64
	return i >= 0 && w < len(b) && b[w]&(1<<(uint(i)%64)) != 0
}

// Clone is a copy that shares nothing with b.
func (b Bitset) Clone() Bitset { return append(Bitset(nil), b...) }

// And keeps only what o also holds.
func (b Bitset) And(o Bitset) {
	for i := range b {
		if i < len(o) {
			b[i] &= o[i]
		} else {
			b[i] = 0
		}
	}
}

// AndNot removes everything o holds.
func (b Bitset) AndNot(o Bitset) {
	for i := range min(len(b), len(o)) {
		b[i] &^= o[i]
	}
}

// Or adds everything o holds. b must be at least as long as o, which every set
// built for one [Network] is.
func (b Bitset) Or(o Bitset) {
	for i := range min(len(b), len(o)) {
		b[i] |= o[i]
	}
}

// Count is the number of members.
func (b Bitset) Count() int {
	n := 0
	for _, w := range b {
		n += bits.OnesCount64(w)
	}
	return n
}

// Empty reports whether the set has no members.
func (b Bitset) Empty() bool {
	for _, w := range b {
		if w != 0 {
			return false
		}
	}
	return true
}

// Each calls fn with every member, in ascending order.
func (b Bitset) Each(fn func(i int)) {
	for wi, w := range b {
		for w != 0 {
			t := bits.TrailingZeros64(w)
			fn(wi*64 + t)
			w &= w - 1
		}
	}
}
