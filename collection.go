package apitobolt

import (
	"encoding/json"
	"errors"
	"strconv"

	bolt "go.etcd.io/bbolt"
)

var (
	ErrNotFound  = errors.New("record not found")
	ErrConflict  = errors.New("unique index conflict")
	ErrBadResult = errors.New("result must be pointer to slice")
)

// bucket naming conventions
const (
	dataBucketPrefix    = "col:"
	indexBucketPrefix   = "idx:"
	indexMetaBucket     = "__indexes__"
	idIndexValuesPrefix = "idmeta:"
)

// Collection is a Mongo-like logical grouping of JSON documents.
type Collection struct {
	name  string
	store *Store
	tx    *bolt.Tx
}

// IndexDef describes a secondary index.
type IndexDef struct {
	Field  string `json:"field"`
	Unique bool   `json:"unique"`
}

// EnsureIndex creates or records an index for the collection.
func (c *Collection) EnsureIndex(field string, unique bool) error {
	return c.withUpdate(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(indexBucketName(c.name, field))); err != nil {
			return err
		}
		meta, err := tx.CreateBucketIfNotExists([]byte(indexMetaBucket))
		if err != nil {
			return err
		}
		defs := c.readIndexDefs(meta)
		updated := false
		for i := range defs {
			if defs[i].Field == field {
				if defs[i].Unique != unique {
					defs[i].Unique = unique
					updated = true
				}
				if updated {
					return c.writeIndexDefs(meta, defs)
				}
				return nil
			}
		}
		defs = append(defs, IndexDef{Field: field, Unique: unique})
		return c.writeIndexDefs(meta, defs)
	})
}

// Save inserts or replaces a document. If the document has no id, a new one is generated.
// Accepted id field names: "id", "ID", or a field tagged with `bolt:"id"`.
func (c *Collection) Save(doc any) (string, error) {
	var newID string
	var outErr error
	err := c.withUpdate(func(tx *bolt.Tx) error {
		dataBkt, err := ensureDataBuckets(tx, c.name)
		if err != nil {
			return err
		}
		id, has := extractID(doc)
		if !has || id == "" {
			seq, err := dataBkt.NextSequence()
			if err != nil {
				return err
			}
			id = strconv.FormatUint(seq, 10)
			if err := setID(doc, id); err != nil {
				return err
			}
		}
		buf, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if err := c.removeOldIndexEntries(tx, id); err != nil {
			return err
		}
		if err := dataBkt.Put([]byte(id), buf); err != nil {
			return err
		}
		if err := c.updateIndexesForDoc(tx, id, buf); err != nil {
			return err
		}
		newID = id
		return nil
	})
	if err != nil {
		outErr = err
	}
	return newID, outErr
}

// FindByID loads a document by id into out.
func (c *Collection) FindByID(id string, out any) error {
	return c.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return ErrNotFound
		}
		data := b.Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, out)
	})
}

// Delete removes a document and cleans up indexes.
func (c *Collection) Delete(id string) error {
	return c.withUpdate(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return ErrNotFound
		}
		data := b.Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}
		if err := c.removeOldIndexEntries(tx, id); err != nil {
			return err
		}
		return b.Delete([]byte(id))
	})
}

// Convenience
func (s *Store) Save(collection string, id string, v any, indexFields ...string) error {
	col := s.Collection(collection)
	for _, f := range indexFields {
		_ = col.EnsureIndex(f, false)
	}
	if id != "" {
		_ = setID(v, id)
	}
	_, err := col.Save(v)
	return err
}

func (s *Store) Get(collection string, id string, v any) error {
	return s.Collection(collection).FindByID(id, v)
}
