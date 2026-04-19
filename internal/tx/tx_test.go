package tx

import (
	"encoding/binary"
	"testing"
)

func TestBuilderShape(t *testing.T) {
	var seed [32]byte
	seed[0] = 1
	b := New(seed)

	sizes := []int{64, 128, 256, 512, 1024}
	for _, size := range sizes {
		buf := make([]byte, size)
		out := b.Build(buf, size)
		if len(out) != size {
			t.Fatalf("size=%d: got len %d", size, len(out))
		}
		if v := binary.LittleEndian.Uint32(out[0:4]); v != 2 {
			t.Errorf("size=%d: version got %d want 2", size, v)
		}
		if out[4] != 1 {
			t.Errorf("size=%d: vin_count got %d want 1", size, out[4])
		}
	}
}

func TestBuilderDeterministic(t *testing.T) {
	var seed [32]byte
	seed[0] = 42
	b1 := New(seed)
	b2 := New(seed)
	buf1 := make([]byte, 256)
	buf2 := make([]byte, 256)
	o1 := b1.Build(buf1, 256)
	o2 := b2.Build(buf2, 256)
	if string(o1) != string(o2) {
		t.Error("same seed produced different output")
	}
}

func TestBuilderMinSize(t *testing.T) {
	var seed [32]byte
	b := New(seed)
	buf := make([]byte, 20)
	out := b.Build(buf, 4) // below min
	if len(out) != 10 {
		t.Errorf("got len %d want 10 (min)", len(out))
	}
}
