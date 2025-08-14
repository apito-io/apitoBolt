package apitobolt

// Legacy compatibility helpers from the initial API

func (s *Store) FindOneBy(collection, field, value string, v any) error {
    return s.Collection(collection).FindOne(field, value, v)
}

func (s *Store) Query(collection string, filter func([]byte) bool, result any) error {
    return s.Collection(collection).QueryFn(func(_ string, data []byte) bool { return filter(data) }, result)
}


