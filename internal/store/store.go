package store

import (
	"fmt"
	"sort"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

type Store struct {
	byID map[uint64]link.Link
}

func New() Store {
	return Store{byID: make(map[uint64]link.Link)}
}
func Add(s Store, l link.Link) error {
	if s.byID == nil {
		s.byID = map[uint64]link.Link{uint64(l.ID): l}
	}
	if _, ok := s.byID[uint64(l.ID)]; ok {
		return fmt.Errorf("ErrDuplicateID: link with id %d already exists", l.ID)
	}

	s.byID[uint64(l.ID)] = l
	return nil
}

func Get(s Store, id uint64) (link.Link, bool) {
	res, ok := s.byID[id]
	return res, ok
}

func All(s Store) []link.Link {
	res := make([]link.Link, 0, len(s.byID))
	for _, l := range s.byID {
		res = append(res, l)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].ID < res[j].ID
	})

	return res
}

func Count(s Store) int {
	return len(s.byID)
}
