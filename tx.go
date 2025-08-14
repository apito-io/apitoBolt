package apitobolt

import (
    bolt "go.etcd.io/bbolt"
)

// Tx is a transaction wrapper providing collection helpers.
type Tx struct {
    tx    *bolt.Tx
    store *Store
}

// Collection returns a handle bound to the transaction.
func (t *Tx) Collection(name string) *Collection {
    return &Collection{name: name, store: t.store, tx: t.tx}
}

// Transactions API similar to Storm's Begin/Commit/Rollback
func (s *Store) Begin(writable bool) (*Tx, error) {
    var rtx *bolt.Tx
    var err error
    if writable {
        rtx, err = s.db.Begin(true)
    } else {
        rtx, err = s.db.Begin(false)
    }
    if err != nil { return nil, err }
    return &Tx{tx: rtx, store: s}, nil
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }


