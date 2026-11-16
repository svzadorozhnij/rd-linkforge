package link

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	cachepkg "github.com/skskuzan/rd-linkforge/pkg/cache"
)

var _ Cache = (*cachepkg.LRU[string, Link])(nil)

type cacheSpy struct {
	entries             map[string]Link
	gets, puts, deletes int
}

func (c *cacheSpy) Get(code string) (Link, bool) {
	c.gets++
	l, ok := c.entries[code]
	return l, ok
}

func (c *cacheSpy) Put(code string, l Link) {
	c.puts++
	if c.entries == nil {
		c.entries = make(map[string]Link)
	}
	c.entries[code] = l
}

func (c *cacheSpy) Delete(code string) {
	c.deletes++
	delete(c.entries, code)
}

type cacheStore struct {
	*hw2Store
	lookups, deletes     int
	lookupErr, deleteErr error
}

func (s *cacheStore) ByCode(ctx context.Context, code string) (Link, error) {
	s.lookups++
	if s.lookupErr != nil {
		return Link{}, s.lookupErr
	}
	return s.hw2Store.ByCode(ctx, code)
}

func (s *cacheStore) Delete(ctx context.Context, id uint64) error {
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.hw2Store.Delete(ctx, id)
}

func cacheFixture(t *testing.T) (*Service, *cacheStore, *cacheSpy, *time.Time) {
	t.Helper()
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	st := &cacheStore{hw2Store: newHW2Store(t)}
	st.byID[1] = Link{ID: 1, Code: "docs", Target: "https://example.com", OwnerID: "owner"}
	c := &cacheSpy{entries: make(map[string]Link)}
	svc := NewService(st, WithFirstID(2), WithCache(c), WithClock(func() time.Time { return now }))
	return svc, st, c, &now
}

func TestServiceCacheMissFillsAndHitSkipsStore(t *testing.T) {
	svc, st, c, _ := cacheFixture(t)
	want := st.byID[1]
	got, err := svc.Resolve(t.Context(), "docs")
	if err != nil || got != want || c.entries["docs"] != want {
		t.Fatalf("miss: got %+v, %v; cached %+v; want %+v", got, err, c.entries["docs"], want)
	}
	st.lookupErr = errors.New("cache hits must bypass this store failure")
	got, err = svc.Resolve(t.Context(), "docs")
	if err != nil || got != want {
		t.Fatalf("hit: got %+v, %v; want %+v, nil", got, err, want)
	}
	if st.lookups != 1 || c.gets != 2 || c.puts != 1 {
		t.Fatalf("lookups=%d, gets=%d, puts=%d; want 1, 2, 1", st.lookups, c.gets, c.puts)
	}
}

func TestServiceCacheDoesNotRememberStoreErrors(t *testing.T) {
	for _, kind := range []string{"missing", "failure"} {
		t.Run(kind, func(t *testing.T) {
			svc, st, c, _ := cacheFixture(t)
			want := ErrNotFound
			if kind == "missing" {
				delete(st.byID, 1)
			} else {
				want = errors.New("store unavailable")
				st.lookupErr = want
			}
			for range 2 {
				if got, err := svc.Resolve(t.Context(), "docs"); got != (Link{}) || !errors.Is(err, want) {
					t.Fatalf("Resolve() = %+v, %v; want zero link and %v", got, err, want)
				}
			}
			if st.lookups != 2 || c.puts != 0 || len(c.entries) != 0 {
				t.Fatalf("failed lookups were cached: store=%d, puts=%d, entries=%v", st.lookups, c.puts, c.entries)
			}
			st.lookupErr = nil
			st.byID[1] = Link{ID: 1, Code: "docs", OwnerID: "owner"}
			if _, err := svc.Resolve(t.Context(), "docs"); err != nil {
				t.Fatal(err)
			}
			if st.lookups != 3 || c.puts != 1 {
				t.Fatal("retry did not fill the cache after the store recovered")
			}
		})
	}
}

func TestServiceCacheExpiry(t *testing.T) {
	for _, prefilled := range []bool{false, true} {
		for _, state := range []string{"forever", "future", "now", "past"} {
			t.Run(fmt.Sprintf("%s/prefilled=%t", state, prefilled), func(t *testing.T) {
				svc, st, c, now := cacheFixture(t)
				l := st.byID[1]
				switch state {
				case "future":
					l.ExpiresAt = now.Add(time.Hour)
				case "now":
					l.ExpiresAt = *now
				case "past":
					l.ExpiresAt = now.Add(-time.Hour)
				}
				st.byID[1] = l
				if prefilled {
					c.entries[l.Code] = l
				}
				got, err := svc.Resolve(t.Context(), l.Code)
				if state == "now" || state == "past" {
					if !errors.Is(err, ErrExpired) || got != (Link{}) {
						t.Fatalf("Resolve() = %+v, %v; want zero link and ErrExpired", got, err)
					}
				} else if err != nil || got != l {
					t.Fatalf("Resolve() = %+v, %v; want %+v, nil", got, err, l)
				}
				wantStoreCalls := 1
				if prefilled {
					wantStoreCalls = 0
				}
				if st.lookups != wantStoreCalls || c.puts != wantStoreCalls {
					t.Fatalf("lookups=%d, puts=%d; want %d each", st.lookups, c.puts, wantStoreCalls)
				}
				if state == "future" {
					*now = l.ExpiresAt
					if _, err := svc.Resolve(t.Context(), l.Code); !errors.Is(err, ErrExpired) {
						t.Fatalf("cached link did not expire as the clock advanced: %v", err)
					}
					if st.lookups != wantStoreCalls {
						t.Fatal("expiry check bypassed the cached link")
					}
				}
			})
		}
	}
}

func TestServiceCacheDeleteInvalidation(t *testing.T) {
	for _, kind := range []string{"success", "empty owner", "forbidden", "unknown", "lookup error", "delete error"} {
		t.Run(kind, func(t *testing.T) {
			svc, st, c, _ := cacheFixture(t)
			cached := st.byID[1]
			// A stale cached owner must never participate in authorization.
			cached.OwnerID = "someone else"
			c.entries["docs"] = cached
			owner := "owner"
			var want error
			switch kind {
			case "empty owner":
				owner, want = "", ErrEmptyOwner
			case "forbidden":
				owner, want = "someone else", ErrForbidden
			case "unknown":
				delete(st.byID, 1)
				want = ErrNotFound
			case "lookup error":
				want = errors.New("lookup failure")
				st.lookupErr = want
			case "delete error":
				want = errors.New("delete failure")
				st.deleteErr = want
			}
			if err := svc.Delete(t.Context(), owner, "docs"); !errors.Is(err, want) {
				t.Fatalf("Delete() = %v, want %v", err, want)
			}
			if c.gets != 0 || c.puts != 0 {
				t.Fatal("Delete consulted or populated the lookup cache")
			}
			if kind == "success" {
				if c.deletes != 1 || len(c.entries) != 0 || len(st.byID) != 0 || st.deletes != 1 {
					t.Fatal("successful Delete did not remove both the stored and cached link")
				}
				if _, err := svc.Resolve(t.Context(), "docs"); !errors.Is(err, ErrNotFound) {
					t.Fatalf("Resolve after Delete = %v, want ErrNotFound", err)
				}
			} else if c.deletes != 0 || c.entries["docs"] != cached {
				t.Fatal("failed Delete changed the cache")
			}
			if kind == "empty owner" && st.lookups != 0 {
				t.Fatal("empty owner reached the store")
			}
		})
	}
}

func TestServiceCanceledContextPrecedesCache(t *testing.T) {
	for _, kind := range []string{"canceled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if kind == "canceled" {
				ctx, cancel = context.WithCancel(t.Context())
				cancel()
			} else {
				ctx, cancel = context.WithDeadline(t.Context(), time.Time{})
			}
			defer cancel()
			c := &cacheSpy{}
			svc := NewService(nil, WithCache(c))
			if _, err := svc.Resolve(ctx, "docs"); !errors.Is(err, ctx.Err()) || errors.Unwrap(err) != nil {
				t.Fatalf("Resolve error = %v, want unchanged %v", err, ctx.Err())
			}
			if err := svc.Delete(ctx, "owner", "docs"); !errors.Is(err, ctx.Err()) || errors.Unwrap(err) != nil {
				t.Fatalf("Delete error = %v, want unchanged %v", err, ctx.Err())
			}
			if c.gets != 0 || c.puts != 0 || c.deletes != 0 {
				t.Fatal("canceled request reached the cache")
			}
		})
	}
}

func TestServiceShortenAndListDoNotTouchCache(t *testing.T) {
	svc, _, c, _ := cacheFixture(t)
	if _, err := svc.Shorten(t.Context(), ShortenRequest{OwnerID: "owner", Target: "https://go.dev"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.List(t.Context(), "owner", Page{}); err != nil {
		t.Fatal(err)
	}
	if c.gets != 0 || c.puts != 0 || c.deletes != 0 || len(c.entries) != 0 {
		t.Fatal("Shorten or List touched the cache")
	}
}

func TestServiceWithRealLRU(t *testing.T) {
	c, err := cachepkg.NewLRU[string, Link](1)
	if err != nil {
		t.Fatal(err)
	}
	st := &cacheStore{hw2Store: newHW2Store(t)}
	svc := NewService(st, WithCache(c))
	links := make([]Link, 0, 2)
	for _, target := range []string{"https://example.com", "https://go.dev"} {
		l, err := svc.Shorten(t.Context(), ShortenRequest{OwnerID: "owner", Target: target})
		if err != nil {
			t.Fatal(err)
		}
		links = append(links, l)
	}
	for _, index := range []int{0, 0, 1, 0} {
		if got, err := svc.Resolve(t.Context(), links[index].Code); err != nil || got != links[index] {
			t.Fatalf("Resolve(%s) = %+v, %v", links[index].Code, got, err)
		}
	}
	if st.lookups != 3 || c.Len() != 1 {
		t.Fatalf("lookups=%d, cache size=%d; want 3, 1", st.lookups, c.Len())
	}
	if err := svc.Delete(t.Context(), "owner", links[0].Code); err != nil || c.Len() != 0 {
		t.Fatalf("Delete() = %v, cache size=%d; want nil, 0", err, c.Len())
	}
}
