package link

import "context"

// Store is the persistence contract Shortener requires. It is declared here,
// in the consuming package; implementations return concrete types.
type Store interface {
	Create(ctx context.Context, l Link) (Link, error)
	ByCode(ctx context.Context, code string) (Link, error)
	ListByOwner(ctx context.Context, ownerID string, page Page) ([]Link, error)
	Delete(ctx context.Context, id uint64) error
}

// NoopStore satisfies Store and persists nothing. Implemented in M1 (memory)
// and M7 (Postgres).
type NoopStore struct{}

var _ Store = NoopStore{}

func (NoopStore) Create(_ context.Context, l Link) (Link, error) { return l, nil }

func (NoopStore) ByCode(context.Context, string) (Link, error) { return Link{}, nil }

func (NoopStore) ListByOwner(context.Context, string, Page) ([]Link, error) { return nil, nil }

func (NoopStore) Delete(context.Context, uint64) error { return nil }
