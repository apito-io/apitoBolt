// Package apitobolt provides a small, production-focused, Mongo-like wrapper on top of bbolt
// with collections, JSON documents, secondary indexes, and ACID CRUD operations.
package apitobolt

import (
    "time"

    bolt "go.etcd.io/bbolt"
)

// Store is the database handle.
type Store struct {
    db *bolt.DB
}

// Open creates (or opens) a bbolt database.
func Open(path string) (*Store, error) {
    db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
    if err != nil {
        return nil, err
    }
    return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// View runs a read-only transaction.
func (s *Store) View(fn func(tx *Tx) error) error {
    return s.db.View(func(btx *bolt.Tx) error { return fn(&Tx{tx: btx, store: s}) })
}

// Update runs a read-write transaction.
func (s *Store) Update(fn func(tx *Tx) error) error {
    return s.db.Update(func(btx *bolt.Tx) error { return fn(&Tx{tx: btx, store: s}) })
}

// Collection returns a handle for a logical collection.
func (s *Store) Collection(name string) *Collection { return &Collection{name: name, store: s} }


