package link

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// hw2Store is a minimal in-memory Store, so these tests do not depend on
// your internal/store package.
type hw2Store struct {
	byID     map[uint64]Link
	creates  int
	lastPage Page
	failWith error
}

func newHW2Store() *hw2Store { return &hw2Store{byID: map[uint64]Link{}} }

func (f *hw2Store) Create(_ context.Context, l Link) (Link, error) {
	f.creates++
	if f.failWith != nil {
		return Link{}, f.failWith
	}
	for _, e := range f.byID {
		if e.Code == l.Code {
			return Link{}, ErrAliasTaken
		}
	}
	f.byID[l.ID] = l
	return l, nil
}

func (f *hw2Store) ByCode(_ context.Context, code string) (Link, error) {
	for _, l := range f.byID {
		if l.Code == code {
			return l, nil
		}
	}
	return Link{}, ErrNotFound
}

func (f *hw2Store) ListByOwner(_ context.Context, _ string, p Page) ([]Link, error) {
	f.lastPage = p
	return nil, nil
}

func (f *hw2Store) Delete(_ context.Context, id uint64) error {
	if _, ok := f.byID[id]; !ok {
		return ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

var _ Shortener = (*Service)(nil)

var hw2Epoch = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func hw2Service(f *hw2Store) (*Service, *time.Time) {
	now := hw2Epoch
	return NewService(f, WithClock(func() time.Time { return now })), &now
}

func hw2Req(target string) ShortenRequest {
	return ShortenRequest{Target: target, OwnerID: "u1"}
}

func TestServiceShortenGenerated(t *testing.T) {
	svc, _ := hw2Service(newHW2Store())
	ctx := context.Background()

	a, err := svc.Shorten(ctx, hw2Req("https://example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || a.Code != "000001" || a.Target != "https://example.com" || a.OwnerID != "u1" || !a.CreatedAt.Equal(hw2Epoch) {
		t.Errorf("first link = %+v", a)
	}
	b, err := svc.Shorten(ctx, hw2Req("https://go.dev"))
	if err != nil || b.ID != 2 || b.Code != "000002" {
		t.Errorf("second link = %+v, %v", b, err)
	}
}

func TestServiceShortenValidation(t *testing.T) {
	ctx := context.Background()
	soon := hw2Epoch.Add(time.Hour)
	tests := []struct {
		name string
		req  ShortenRequest
		want error
	}{
		{"no owner", ShortenRequest{Target: "https://a.io"}, ErrEmptyOwner},
		{"empty target", ShortenRequest{OwnerID: "u1"}, ErrEmptyTarget},
		{"bad scheme", hw2Req("ftp://a.io"), ErrUnsupportedScheme},
		{"expiry now", ShortenRequest{Target: "https://a.io", OwnerID: "u1", ExpiresAt: hw2Epoch}, ErrInvalidExpiry},
		{"expiry past", ShortenRequest{Target: "https://a.io", OwnerID: "u1", ExpiresAt: hw2Epoch.Add(-time.Second)}, ErrInvalidExpiry},
		{"alias short", ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "ab"}, ErrInvalidAlias},
		{"alias chars", ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "bad alias"}, ErrInvalidAlias},
		{"alias long", ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: strings.Repeat("a", 33)}, ErrInvalidAlias},
		{"alias reserved", ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "api"}, ErrReservedAlias},
		{"alias reserved case", ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "Metrics"}, ErrReservedAlias},
	}
	for _, tc := range tests {
		f := newHW2Store()
		svc, _ := hw2Service(f)
		if _, err := svc.Shorten(ctx, tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if f.creates != 0 {
			t.Errorf("%s: store was called for an invalid request", tc.name)
		}
		// A failed request must not consume an ID.
		if l, err := svc.Shorten(ctx, ShortenRequest{Target: "https://ok.io", OwnerID: "u1", ExpiresAt: soon}); err != nil || l.ID != 1 {
			t.Errorf("%s: next link = %+v, %v; want ID 1", tc.name, l, err)
		}
	}
}

func TestServiceAlias(t *testing.T) {
	svc, _ := hw2Service(newHW2Store())
	ctx := context.Background()
	req := ShortenRequest{Target: "https://example.com", OwnerID: "u1", Alias: "my-link_1"}

	l, err := svc.Shorten(ctx, req)
	if err != nil || l.Code != "my-link_1" || l.ID != 1 {
		t.Fatalf("alias link = %+v, %v", l, err)
	}

	_, err = svc.Shorten(ctx, req)
	var ae *AliasError
	if !errors.Is(err, ErrAliasTaken) || !errors.As(err, &ae) || ae.Alias != "my-link_1" {
		t.Fatalf("duplicate alias err = %v", err)
	}

	next, err := svc.Shorten(ctx, hw2Req("https://go.dev"))
	if err != nil || next.ID != 2 {
		t.Errorf("link after a taken alias = %+v, %v; want ID 2", next, err)
	}

	_, err = svc.Shorten(ctx, ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "ab"})
	if !errors.As(err, &ae) || ae.Alias != "ab" || !errors.Is(err, ErrInvalidAlias) {
		t.Errorf("invalid alias err = %v", err)
	}
}

func TestServiceGeneratedCodeCollision(t *testing.T) {
	svc, _ := hw2Service(newHW2Store())
	ctx := context.Background()
	for _, alias := range []string{"000004", "000005", "000006"} {
		if _, err := svc.Shorten(ctx, ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: alias}); err != nil {
			t.Fatal(err)
		}
	}
	// IDs 1-3 are used; ID 4 derives "000004", which is taken, then 5 and 6.
	if _, err := svc.Shorten(ctx, hw2Req("https://example.com")); !errors.Is(err, ErrAliasTaken) {
		t.Fatalf("err = %v, want ErrAliasTaken after three collisions", err)
	}
	// Each attempt consumed an ID, so the next free code is derived from 7.
	l, err := svc.Shorten(ctx, hw2Req("https://example.com"))
	if err != nil || l.ID != 7 || l.Code != "000007" {
		t.Errorf("link = %+v, %v; want ID 7", l, err)
	}
}

func TestServiceCollisionRetrySucceeds(t *testing.T) {
	svc, _ := hw2Service(newHW2Store())
	ctx := context.Background()
	if _, err := svc.Shorten(ctx, ShortenRequest{Target: "https://a.io", OwnerID: "u1", Alias: "000002"}); err != nil {
		t.Fatal(err)
	}
	l, err := svc.Shorten(ctx, hw2Req("https://example.com"))
	if err != nil || l.ID != 3 || l.Code != "000003" {
		t.Errorf("link = %+v, %v; want ID 3 after one collision", l, err)
	}
}

func TestServiceStoreErrorIsWrapped(t *testing.T) {
	boom := errors.New("boom")
	f := newHW2Store()
	f.failWith = boom
	svc, _ := hw2Service(f)
	ctx := context.Background()

	if _, err := svc.Shorten(ctx, hw2Req("https://example.com")); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the store error", err)
	}
	f.failWith = nil
	if l, err := svc.Shorten(ctx, hw2Req("https://example.com")); err != nil || l.ID != 1 {
		t.Errorf("link after a store failure = %+v, %v; want ID 1", l, err)
	}
}

func TestServiceResolve(t *testing.T) {
	svc, now := hw2Service(newHW2Store())
	ctx := context.Background()
	expires := hw2Epoch.Add(time.Hour)

	temp, _ := svc.Shorten(ctx, ShortenRequest{Target: "https://a.io", OwnerID: "u1", ExpiresAt: expires})
	forever, _ := svc.Shorten(ctx, hw2Req("https://b.io"))

	got, err := svc.Resolve(ctx, temp.Code)
	if err != nil || got.Target != "https://a.io" {
		t.Fatalf("Resolve = %+v, %v", got, err)
	}
	if _, err := svc.Resolve(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown code err = %v, want ErrNotFound", err)
	}

	*now = expires.Add(-time.Nanosecond)
	if _, err := svc.Resolve(ctx, temp.Code); err != nil {
		t.Errorf("just before expiry: %v", err)
	}
	*now = expires
	if _, err := svc.Resolve(ctx, temp.Code); !errors.Is(err, ErrExpired) {
		t.Errorf("at expiry err = %v, want ErrExpired", err)
	}
	*now = hw2Epoch.AddDate(10, 0, 0)
	if _, err := svc.Resolve(ctx, forever.Code); err != nil {
		t.Errorf("link without expiry: %v", err)
	}
}

func TestServiceDelete(t *testing.T) {
	svc, _ := hw2Service(newHW2Store())
	ctx := context.Background()
	l, _ := svc.Shorten(ctx, hw2Req("https://a.io"))

	if err := svc.Delete(ctx, "", l.Code); !errors.Is(err, ErrEmptyOwner) {
		t.Errorf("empty owner err = %v", err)
	}
	if err := svc.Delete(ctx, "intruder", l.Code); !errors.Is(err, ErrForbidden) {
		t.Errorf("foreign owner err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Resolve(ctx, l.Code); err != nil {
		t.Errorf("link must survive a forbidden delete: %v", err)
	}
	if err := svc.Delete(ctx, "u1", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown code err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, "u1", l.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, l.Code); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete err = %v, want ErrNotFound", err)
	}
}

func TestServiceListNormalizesPage(t *testing.T) {
	f := newHW2Store()
	svc, _ := hw2Service(f)
	ctx := context.Background()

	if _, err := svc.List(ctx, "", Page{}); !errors.Is(err, ErrEmptyOwner) {
		t.Errorf("empty owner err = %v", err)
	}
	for _, tc := range []struct{ in, want int }{{0, DefaultPageLimit}, {-3, DefaultPageLimit}, {5, 5}, {1000, MaxPageLimit}} {
		if _, err := svc.List(ctx, "u1", Page{Cursor: "7", Limit: tc.in}); err != nil {
			t.Fatal(err)
		}
		if f.lastPage.Limit != tc.want || f.lastPage.Cursor != "7" {
			t.Errorf("limit %d reached the store as %+v, want limit %d and cursor 7", tc.in, f.lastPage, tc.want)
		}
	}
}

func TestPageNormalize(t *testing.T) {
	tests := []struct{ in, want int }{{-1, DefaultPageLimit}, {0, DefaultPageLimit}, {1, 1}, {100, 100}, {101, MaxPageLimit}}
	for _, tc := range tests {
		in := Page{Cursor: "c", Limit: tc.in}
		got := in.Normalize()
		if got.Limit != tc.want || got.Cursor != "c" {
			t.Errorf("Normalize(%+v) = %+v", in, got)
		}
		if in.Limit != tc.in {
			t.Errorf("Normalize must not modify its receiver")
		}
	}
}

func TestPageAfterID(t *testing.T) {
	for cursor, want := range map[string]uint64{"": 0, "1": 1, "10": 62, "zz": 3843} {
		if got, err := (Page{Cursor: cursor}).AfterID(); err != nil || got != want {
			t.Errorf("AfterID(%q) = %d, %v; want %d", cursor, got, err, want)
		}
	}
	for _, cursor := range []string{"a-b", "!", "zzzzzzzzzzz"} {
		if _, err := (Page{Cursor: cursor}).AfterID(); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("AfterID(%q) err = %v, want ErrInvalidCursor", cursor, err)
		}
	}
}

func TestAliasError(t *testing.T) {
	err := error(&AliasError{Alias: "docs", Err: ErrAliasTaken})
	if !errors.Is(err, ErrAliasTaken) {
		t.Error("AliasError must unwrap to its cause")
	}
	if !strings.Contains(err.Error(), "docs") {
		t.Errorf("message %q does not mention the alias", err.Error())
	}
}
