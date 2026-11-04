package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

func TestStoreContext(t *testing.T) {
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
			st := New()
			existing := link.Link{ID: 1, Code: "existing", OwnerID: "owner"}
			if _, err := st.Create(t.Context(), existing); err != nil {
				t.Fatal(err)
			}
			before := st.All()
			for _, tc := range []struct {
				name string
				call func() error
			}{
				{"Create valid", func() error {
					_, err := st.Create(ctx, link.Link{ID: 2, Code: "new"})
					return err
				}},
				{"Create duplicate", func() error { _, err := st.Create(ctx, existing); return err }},
				{"ByCode", func() error { _, err := st.ByCode(ctx, "missing"); return err }},
				{"ListByOwner", func() error { _, err := st.ListByOwner(ctx, "owner", link.Page{Cursor: "!"}); return err }},
				{"Delete existing", func() error { return st.Delete(ctx, 1) }},
				{"Delete missing", func() error { return st.Delete(ctx, 2) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					err := tc.call()
					if !errors.Is(err, ctx.Err()) || errors.Unwrap(err) != nil {
						t.Fatalf("error = %v, want unchanged context error %v", err, ctx.Err())
					}
					if !reflect.DeepEqual(st.All(), before) {
						t.Fatal("a canceled call changed the store")
					}
					if got, err := st.ByCode(t.Context(), existing.Code); err != nil || got != existing {
						t.Fatalf("existing code changed: %+v, %v", got, err)
					}
				})
			}
		})
	}
}
