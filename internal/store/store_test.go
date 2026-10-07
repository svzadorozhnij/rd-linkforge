package store

import (
	"testing"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

func TestNew(t *testing.T) {
	s := New()
	if s.byID == nil {
		t.Error("IsEmpty store map 'byID'")
	}
}

func TestAddAndGet(t *testing.T) {
	s := New()
	l := link.Link{ID: 42}

	err := Add(s, l)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = Add(s, l)
	if err == nil {
		t.Error("Wait ErrDuplicateID")
	}

	found, ok := Get(s, 42)
	if !ok {
		t.Error("Added early ID not found")
	}
	if found.ID != l.ID {
		t.Errorf("Wait ID %d, get %d", l.ID, found.ID)
	}
}

func TestCount(t *testing.T) {
	s := New()
	if Count(s) != 0 {
		t.Errorf("Need to be empty -> has %d", Count(s))
	}

	_ = Add(s, link.Link{ID: 1})
	_ = Add(s, link.Link{ID: 2})
	_ = Add(s, link.Link{ID: 2})
	_ = Add(s, link.Link{ID: 2})
	_ = Add(s, link.Link{ID: 2})
	_ = Add(s, link.Link{ID: 2})
	_ = Add(s, link.Link{ID: 5})

	if Count(s) != 3 {
		t.Errorf("Wait 2 items in store -> has %d", Count(s))
	}
}

func TestAllSorted(t *testing.T) {
	s := New()

	linksToAdd := []link.Link{
		{ID: 100},
		{ID: 5},
		{ID: 50},
		{ID: 1},
	}

	for _, l := range linksToAdd {
		_ = Add(s, l)
	}

	allLinks := All(s)

	expectedIDs := []uint64{1, 5, 50, 100}
	for i, l := range allLinks {
		if uint64(l.ID) != expectedIDs[i] {
			t.Errorf("By index %d wait ID %d, but has %d", i, expectedIDs[i], l.ID)
		}
	}
}
