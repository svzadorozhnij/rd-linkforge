package link

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceContext(t *testing.T) {
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
			// A nil store makes any accidental call to persistence fail immediately.
			svc := NewService(nil)
			for _, tc := range []struct {
				name string
				call func() error
			}{
				{"Shorten valid", func() error {
					_, err := svc.Shorten(ctx, ShortenRequest{Target: "https://example.com", OwnerID: "owner"})
					return err
				}},
				{"Shorten invalid", func() error { _, err := svc.Shorten(ctx, ShortenRequest{}); return err }},
				{"Resolve", func() error { _, err := svc.Resolve(ctx, "missing"); return err }},
				{"List", func() error { _, err := svc.List(ctx, "", Page{Cursor: "!"}); return err }},
				{"Delete", func() error { return svc.Delete(ctx, "", "missing") }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					err := tc.call()
					if !errors.Is(err, ctx.Err()) || errors.Unwrap(err) != nil {
						t.Fatalf("error = %v, want unchanged context error %v", err, ctx.Err())
					}
				})
			}
		})
	}
}

func TestCanceledShortenDoesNotConsumeID(t *testing.T) {
	st := newHW2Store()
	svc := NewService(st, WithFirstID(42))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := ShortenRequest{Target: "https://example.com", OwnerID: "owner"}
	if _, err := svc.Shorten(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if st.creates != 0 {
		t.Fatal("a canceled Shorten reached the store")
	}
	got, err := svc.Shorten(t.Context(), req)
	if err != nil || got.ID != 42 {
		t.Fatalf("first successful Shorten: ID = %d, error = %v; want ID 42", got.ID, err)
	}
}
