package apitobolt

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// Update updates non-zero fields from doc, keeping the same id.
func (c *Collection) Update(doc any) error {
	id, ok := extractID(doc)
	if !ok || id == "" {
		return errors.New("missing id")
	}
	return c.withUpdate(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return ErrNotFound
		}
		cur := b.Get([]byte(id))
		if cur == nil {
			return ErrNotFound
		}
		var m map[string]any
		if err := json.Unmarshal(cur, &m); err != nil {
			return err
		}
		rv := reflect.ValueOf(doc)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			f := rt.Field(i)
			name := f.Tag.Get("json")
			if name == "" || name == "-" {
				name = f.Name
			}
			if strings.Contains(name, ",") {
				name = strings.Split(name, ",")[0]
			}
			if strings.EqualFold(name, "id") {
				continue
			}
			fv := rv.Field(i)
			if !isZeroValue(fv) {
				m[name] = fv.Interface()
			}
		}
		buf, _ := json.Marshal(m)
		if err := c.removeOldIndexEntries(tx, id); err != nil {
			return err
		}
		if err := b.Put([]byte(id), buf); err != nil {
			return err
		}
		return c.updateIndexesForDoc(tx, id, buf)
	})
}

// UpdateField updates a single field to the provided value (including zero values).
func (c *Collection) UpdateField(idDoc any, field string, value any) error {
	id, ok := extractID(idDoc)
	if !ok || id == "" {
		return errors.New("missing id")
	}
	return c.withUpdate(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return ErrNotFound
		}
		cur := b.Get([]byte(id))
		if cur == nil {
			return ErrNotFound
		}
		var m map[string]any
		if err := json.Unmarshal(cur, &m); err != nil {
			return err
		}
		m[field] = value
		buf, _ := json.Marshal(m)
		if err := c.removeOldIndexEntries(tx, id); err != nil {
			return err
		}
		if err := b.Put([]byte(id), buf); err != nil {
			return err
		}
		return c.updateIndexesForDoc(tx, id, buf)
	})
}

// DeleteStruct deletes by the id found in the struct.
func (c *Collection) DeleteStruct(doc any) error {
	id, ok := extractID(doc)
	if !ok || id == "" {
		return errors.New("missing id")
	}
	return c.Delete(id)
}

// Init ensures underlying buckets exist.
func (c *Collection) Init() error {
	return c.withUpdate(func(tx *bolt.Tx) error { _, err := ensureDataBuckets(tx, c.name); return err })
}

// Drop removes the collection data and its indexes.
func (c *Collection) Drop() error {
	return c.withUpdate(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte(dataBucketName(c.name))); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
			return err
		}
		if err := tx.DeleteBucket([]byte(idIndexValuesBucketName(c.name))); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
			return err
		}
		meta := tx.Bucket([]byte(indexMetaBucket))
		if meta != nil {
			defs := c.readIndexDefs(meta)
			for _, d := range defs {
				_ = tx.DeleteBucket([]byte(indexBucketName(c.name, d.Field)))
			}
			_ = meta.Delete([]byte(c.name))
		}
		return nil
	})
}

// ReIndex rebuilds all indexes for the collection.
func (c *Collection) ReIndex() error {
	return c.withUpdate(func(tx *bolt.Tx) error {
		meta := tx.Bucket([]byte(indexMetaBucket))
		var defs []IndexDef
		if meta != nil {
			defs = c.readIndexDefs(meta)
		}
		for _, d := range defs {
			_ = tx.DeleteBucket([]byte(indexBucketName(c.name, d.Field)))
		}
		for _, d := range defs {
			if _, err := tx.CreateBucketIfNotExists([]byte(indexBucketName(c.name, d.Field))); err != nil {
				return err
			}
		}
		_ = tx.DeleteBucket([]byte(idIndexValuesBucketName(c.name)))
		if _, err := tx.CreateBucketIfNotExists([]byte(idIndexValuesBucketName(c.name))); err != nil {
			return err
		}
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error { return c.updateIndexesForDoc(tx, string(k), v) })
	})
}
