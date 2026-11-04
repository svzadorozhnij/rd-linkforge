package link

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/skskuzan/rd-linkforge/internal/base62"
)

type Service struct {
	store  Store
	nextID uint64
	now    func() time.Time
}

var _ Shortener = (*Service)(nil)

func (s *Service) Shorten(ctx context.Context, req ShortenRequest) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}
	if req.OwnerID == "" {
		return Link{}, ErrEmptyOwner
	}
	link, err := New(s.nextID, req.Target)
	if err != nil {
		return Link{}, err
	}
	now := s.now()
	if !req.ExpiresAt.IsZero() && !req.ExpiresAt.After(now) {
		return Link{}, ErrInvalidExpiry
	}
	if req.Alias != "" {
		if err := validateAlias(req.Alias); err != nil {
			return Link{}, &AliasError{Alias: req.Alias, Err: err}
		}
	}

	link.OwnerID = req.OwnerID
	link.ExpiresAt = req.ExpiresAt
	link.CreatedAt = now.UTC()
	for range 3 {
		link.ID = s.nextID
		link.Code = req.Alias
		if link.Code == "" {
			link.Code = base62.EncodeWidth(link.ID, 6)
		}
		created, err := s.store.Create(ctx, link)
		if err == nil {
			s.nextID++
			return created, nil
		}
		if !errors.Is(err, ErrAliasTaken) {
			return Link{}, fmt.Errorf("create link: %w", err)
		}
		if req.Alias != "" {
			return Link{}, &AliasError{Alias: req.Alias, Err: err}
		}
		s.nextID++
	}
	return Link{}, &AliasError{Alias: link.Code, Err: ErrAliasTaken}
}

func (s *Service) Resolve(ctx context.Context, code string) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}
	l, err := s.store.ByCode(ctx, code)
	if err != nil {
		return Link{}, fmt.Errorf("resolve link %q: %w", code, err)
	}
	if !l.ExpiresAt.IsZero() && !s.now().Before(l.ExpiresAt) {
		return Link{}, ErrExpired
	}
	return l, nil
}

func (s *Service) List(ctx context.Context, ownerID string, page Page) ([]Link, error) {
	if err := ctx.Err(); err != nil {
		return []Link{}, err
	}
	if ownerID == "" {
		return nil, ErrEmptyOwner
	}
	return s.store.ListByOwner(ctx, ownerID, page.Normalize())
}

func (s *Service) Delete(ctx context.Context, ownerID, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ownerID == "" {
		return ErrEmptyOwner
	}
	l, err := s.store.ByCode(ctx, code)
	if err != nil {
		return fmt.Errorf("find link to delete %q: %w", code, err)
	}
	if l.OwnerID != ownerID {
		return ErrForbidden
	}
	if err := s.store.Delete(ctx, l.ID); err != nil {
		return fmt.Errorf("delete link %q: %w", code, err)
	}
	return nil
}

type Option func(*Service)

// WithClock replaces the time source, so that tests can control time.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		s.now = now
	}
}

// WithFirstID sets the ID handed out first. Values below 1 are ignored.
func WithFirstID(id uint64) Option {
	return func(s *Service) {
		if id >= 1 {
			s.nextID = id
		}
	}
}

func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store, nextID: 1, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func validateAlias(alias string) error {
	switch strings.ToLower(alias) {
	case "api", "ws", "healthz", "readyz", "metrics", "admin":
		return ErrReservedAlias
	}
	if len(alias) < 3 || len(alias) > 32 {
		return ErrInvalidAlias
	}
	for _, c := range alias {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return ErrInvalidAlias
	}
	return nil
}
