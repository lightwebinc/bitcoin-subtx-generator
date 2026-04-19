package subtree

import "testing"

func TestPoolDeterministic(t *testing.T) {
	p1 := New(8, []byte("test-seed"))
	p2 := New(8, []byte("test-seed"))
	if p1.Len() != 8 || p2.Len() != 8 {
		t.Fatalf("wrong length")
	}
	for i := 0; i < 8; i++ {
		if p1.At(i) != p2.At(i) {
			t.Errorf("id[%d] mismatch across pools", i)
		}
	}
}

func TestPoolUniqueIDs(t *testing.T) {
	p := New(16, []byte("seed"))
	seen := make(map[ID]bool)
	for i := 0; i < p.Len(); i++ {
		id := p.At(i)
		if seen[id] {
			t.Errorf("duplicate id at %d", i)
		}
		seen[id] = true
	}
}

func TestPoolEmpty(t *testing.T) {
	p := New(0, nil)
	if p.Len() != 0 {
		t.Errorf("expected empty pool")
	}
	var zero ID
	if p.Pick(42) != zero {
		t.Errorf("Pick on empty pool must return zero")
	}
}

func TestPoolPickModulo(t *testing.T) {
	p := New(4, []byte("seed"))
	if p.Pick(0) != p.Pick(4) {
		t.Error("Pick(0) should equal Pick(4)")
	}
}
