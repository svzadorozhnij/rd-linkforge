package link

// Cache is the lookup cache Resolve consults before the store.
type Cache interface {
	Get(code string) (Link, bool)
	Put(code string, l Link)
	Delete(code string)
}

// WithCache makes Resolve look in c first and fill it after a store hit.
func WithCache(c Cache) Option {
	return func(s *Service) {
		s.cache = c
	}
}
