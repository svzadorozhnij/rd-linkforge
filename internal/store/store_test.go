package store

import (
	"context"
	"errors"
	"testing"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

func TestNew(t *testing.T) {
	s := New()
	if s.byID == nil {
		t.Error("IsEmpty store map 'byID'")
	}
}

func TestCreateAndGet(t *testing.T) {
	s := New()
	l := link.Link{ID: 42}

	_, err := s.Create(context.Background(), l)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, err = s.Create(context.Background(), l)
	if !errors.Is(err, ErrDuplicateID) {
		t.Error("Wait ErrDuplicateID")
	}

	found, ok := s.Get(42)
	if !ok {
		t.Error("Added early ID not found")
	}
	if found.ID != l.ID {
		t.Errorf("Wait ID %d, get %d", l.ID, found.ID)
	}
}

func TestCount(t *testing.T) {
	s := New()
	if s.Count() != 0 {
		t.Errorf("Need to be empty -> has %d", s.Count())
	}

	for _, l := range []link.Link{{ID: 1, Code: "one"}, {ID: 2, Code: "two"}, {ID: 5, Code: "five"}} {
		if _, err := s.Create(context.Background(), l); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		if _, err := s.Create(context.Background(), link.Link{ID: 2, Code: "two"}); !errors.Is(err, ErrDuplicateID) {
			t.Fatalf("Expected ErrDuplicateID, got %v", err)
		}
	}

	if s.Count() != 3 {
		t.Errorf("Wait 3 items in store -> has %d", s.Count())
	}
}

func TestAllSorted(t *testing.T) {
	s := New()

	linksToAdd := []link.Link{
		{ID: 100, Code: "hundred"},
		{ID: 5, Code: "five"},
		{ID: 50, Code: "fifty"},
		{ID: 1, Code: "one"},
	}

	for _, l := range linksToAdd {
		if _, err := s.Create(context.Background(), l); err != nil {
			t.Fatal(err)
		}
	}

	allLinks := s.All()

	expectedIDs := []uint64{1, 5, 50, 100}
	if len(allLinks) != len(expectedIDs) {
		t.Fatalf("Expected %d links, got %d", len(expectedIDs), len(allLinks))
	}
	for i, l := range allLinks {
		if l.ID != expectedIDs[i] {
			t.Errorf("By index %d wait ID %d, but has %d", i, expectedIDs[i], l.ID)
		}
	}
}
