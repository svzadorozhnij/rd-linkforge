package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

var _ link.Store = (*Store)(nil)

func hw2Link(id uint64, code, owner string) link.Link {
	return link.Link{ID: id, Code: code, Target: "https://example.com", OwnerID: owner}
}

func TestStoreCreateAndByCode(t *testing.T) {
	ctx := context.Background()
	s := New()

	got, err := s.Create(ctx, hw2Link(1, "abc", "u1"))
	if err != nil || got.ID != 1 || got.Code != "abc" {
		t.Fatalf("Create = %+v, %v", got, err)
	}
	if l, err := s.ByCode(ctx, "abc"); err != nil || l.ID != 1 {
		t.Errorf("ByCode = %+v, %v", l, err)
	}
	if _, err := s.ByCode(ctx, "nope"); !errors.Is(err, link.ErrNotFound) {
		t.Errorf("unknown code err = %v, want link.ErrNotFound", err)
	}
	if _, err := s.Create(ctx, hw2Link(1, "other", "u1")); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("duplicate id err = %v, want ErrDuplicateID", err)
	}
	if _, err := s.Create(ctx, hw2Link(2, "abc", "u1")); !errors.Is(err, link.ErrAliasTaken) {
		t.Errorf("duplicate code err = %v, want link.ErrAliasTaken", err)
	}
	if s.Count() != 1 {
		t.Errorf("a rejected Create must not change the store; Count = %d", s.Count())
	}
}

func TestStoreGetAllCount(t *testing.T) {
	ctx := context.Background()
	s := New()
	for _, id := range []uint64{3, 1, 2} {
		if _, err := s.Create(ctx, hw2Link(id, string(rune('a'+id)), "u1")); err != nil {
			t.Fatal(err)
		}
	}
	if s.Count() != 3 {
		t.Errorf("Count = %d", s.Count())
	}
	if l, ok := s.Get(2); !ok || l.ID != 2 {
		t.Errorf("Get(2) = %+v, %v", l, ok)
	}
	if _, ok := s.Get(9); ok {
		t.Error("Get(9) reported a link that does not exist")
	}
	var ids []uint64
	for _, l := range s.All() {
		ids = append(ids, l.ID)
	}
	if !slices.Equal(ids, []uint64{1, 2, 3}) {
		t.Errorf("All order = %v, want [1 2 3]", ids)
	}
}

func TestStoreDelete(t *testing.T) {
	ctx := context.Background()
	s := New()
	_, _ = s.Create(ctx, hw2Link(1, "abc", "u1"))

	if err := s.Delete(ctx, 9); !errors.Is(err, link.ErrNotFound) {
		t.Errorf("unknown id err = %v, want link.ErrNotFound", err)
	}
	if err := s.Delete(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ByCode(ctx, "abc"); !errors.Is(err, link.ErrNotFound) {
		t.Errorf("deleted link still found: %v", err)
	}
	if _, err := s.Create(ctx, hw2Link(2, "abc", "u1")); err != nil {
		t.Errorf("a deleted link must free its code: %v", err)
	}
}

func TestStoreListByOwner(t *testing.T) {
	ctx := context.Background()
	s := New()
	for id := uint64(1); id <= 7; id++ {
		owner := "a"
		if id%2 == 0 {
			owner = "b"
		}
		if _, err := s.Create(ctx, hw2Link(id, "code"+string(rune('0'+id)), owner)); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(owner string, p link.Page) []uint64 {
		t.Helper()
		ls, err := s.ListByOwner(ctx, owner, p)
		if err != nil {
			t.Fatalf("ListByOwner(%q, %+v): %v", owner, p, err)
		}
		var out []uint64
		for _, l := range ls {
			out = append(out, l.ID)
		}
		return out
	}

	if got := ids("a", link.Page{Limit: 2}); !slices.Equal(got, []uint64{1, 3}) {
		t.Errorf("first page = %v", got)
	}
	if got := ids("a", link.Page{Cursor: "3", Limit: 2}); !slices.Equal(got, []uint64{5, 7}) {
		t.Errorf("second page = %v", got)
	}
	if got := ids("a", link.Page{Cursor: "7", Limit: 2}); len(got) != 0 {
		t.Errorf("page after the last link = %v", got)
	}
	if got := ids("b", link.Page{}); !slices.Equal(got, []uint64{2, 4, 6}) {
		t.Errorf("default limit page = %v", got)
	}
	if got := ids("nobody", link.Page{}); len(got) != 0 {
		t.Errorf("unknown owner = %v", got)
	}
	if _, err := s.ListByOwner(ctx, "a", link.Page{Cursor: "!"}); !errors.Is(err, link.ErrInvalidCursor) {
		t.Errorf("invalid cursor err = %v, want link.ErrInvalidCursor", err)
	}
}

func TestServiceOnMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := New()
	svc := link.NewService(s)

	for _, target := range []string{"https://a.io", "https://b.io", "https://c.io"} {
		if _, err := svc.Shorten(ctx, link.ShortenRequest{Target: target, OwnerID: "u1"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Shorten(ctx, link.ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "docs"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Shorten(ctx, link.ShortenRequest{Target: "https://x.io", OwnerID: "u1", Alias: "docs"}); !errors.Is(err, link.ErrAliasTaken) {
		t.Errorf("duplicate alias err = %v", err)
	}

	page, err := svc.List(ctx, "u1", link.Page{Limit: 2})
	if err != nil || len(page) != 2 || page[1].Code != "000002" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	rest, err := svc.List(ctx, "u1", link.Page{Cursor: "2", Limit: 10})
	if err != nil || len(rest) != 2 || rest[1].Code != "docs" {
		t.Fatalf("second page = %+v, %v", rest, err)
	}

	if l, err := svc.Resolve(ctx, "docs"); err != nil || l.Target != "https://a.io" {
		t.Errorf("Resolve(docs) = %+v, %v", l, err)
	}
	if err := svc.Delete(ctx, "u1", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, "docs"); !errors.Is(err, link.ErrNotFound) {
		t.Errorf("Resolve after delete err = %v", err)
	}
	if s.Count() != 3 {
		t.Errorf("Count = %d, want 3", s.Count())
	}
}
