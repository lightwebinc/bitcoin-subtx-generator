package seq

import (
	"testing"
	"time"
)

func TestMonotonic(t *testing.T) {
	a := New(Config{Start: 1})
	prev := uint64(0)
	for i := 0; i < 100; i++ {
		s := a.Next()
		if s <= prev {
			t.Fatalf("not monotonic: %d after %d", s, prev)
		}
		prev = s
	}
}

func TestGapPermanent(t *testing.T) {
	a := New(Config{Start: 1, GapEvery: 5, GapSize: 2, GapDelay: 0})
	seen := make(map[uint64]bool)
	for i := 0; i < 20; i++ {
		seen[a.Next()] = true
	}
	// Every 5 allocations skip 2. At 20 allocations: 4 gaps × 2 skipped = 8 missing.
	// Issued = 20; reserved-and-skipped = 8. next counter advanced by 28.
	missing := 0
	for n := uint64(1); n <= 28; n++ {
		if !seen[n] {
			missing++
		}
	}
	if missing != 8 {
		t.Errorf("want 8 missing seqs, got %d", missing)
	}
	if a.Pending() != 0 {
		t.Errorf("GapDelay=0 should not queue pending")
	}
}

func TestGapDelayed(t *testing.T) {
	a := New(Config{Start: 1, GapEvery: 3, GapSize: 1, GapDelay: 5 * time.Millisecond})
	for i := 0; i < 9; i++ {
		a.Next()
	}
	if a.Pending() != 3 {
		t.Errorf("want 3 pending, got %d", a.Pending())
	}
	due := a.DueRetransmits(time.Now())
	if len(due) != 0 {
		t.Errorf("too early, got %d due", len(due))
	}
	time.Sleep(10 * time.Millisecond)
	due = a.DueRetransmits(time.Now())
	if len(due) != 3 {
		t.Errorf("want 3 due, got %d", len(due))
	}
	if a.Pending() != 0 {
		t.Errorf("expected queue drained, got %d", a.Pending())
	}
}
