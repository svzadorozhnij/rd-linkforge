package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

var ErrDuplicateID = errors.New("store: duplicate id")

type Store struct {
	byID   map[uint64]link.Link
	byCode map[string]uint64
}

var _ link.Store = (*Store)(nil)

func New() *Store {
	return &Store{byID: make(map[uint64]link.Link), byCode: make(map[string]uint64)}
}

func (s *Store) Create(_ context.Context, l link.Link) (link.Link, error) {
	if _, ok := s.byID[l.ID]; ok {
		return link.Link{}, fmt.Errorf("%w: %d", ErrDuplicateID, l.ID)
	}
	if _, ok := s.byCode[l.Code]; ok {
		return link.Link{}, fmt.Errorf("code %q: %w", l.Code, link.ErrAliasTaken)
	}
	if s.byID == nil {
		s.byID = make(map[uint64]link.Link)
	}
	if s.byCode == nil {
		s.byCode = make(map[string]uint64)
	}
	s.byID[l.ID] = l
	s.byCode[l.Code] = l.ID
	return l, nil
}

func (s *Store) Get(id uint64) (link.Link, bool) {
	res, ok := s.byID[id]
	return res, ok
}

func (s *Store) All() []link.Link {
	res := make([]link.Link, 0, len(s.byID))
	for _, id := range sortedIDs(s.byID) {
		res = append(res, s.byID[id])
	}

	return res
}

func (s *Store) Count() int {
	return len(s.byID)
}

func (s *Store) ByCode(_ context.Context, code string) (link.Link, error) {
	id, ok := s.byCode[code]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	return s.byID[id], nil
}

func (s *Store) ListByOwner(_ context.Context, ownerID string, page link.Page) ([]link.Link, error) {
	page = page.Normalize()
	afterID, err := page.AfterID()
	if err != nil {
		return nil, err
	}
	var res []link.Link

	for _, id := range sortedIDs(s.byID) {
		l := s.byID[id]
		if id > afterID && l.OwnerID == ownerID {
			res = append(res, l)
			if len(res) == page.Limit {
				break
			}
		}
	}

	return res, nil
}

func (s *Store) Delete(_ context.Context, id uint64) error {
	l, ok := s.byID[id]
	if !ok {
		return link.ErrNotFound
	}
	delete(s.byID, id)
	delete(s.byCode, l.Code)
	return nil
}

func sortedIDs(m map[uint64]link.Link) []uint64 {
	ids := make([]uint64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
