package apitobolt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

func dataBucketName(col string) string          { return dataBucketPrefix + col }
func indexBucketName(col, field string) string  { return indexBucketPrefix + col + ":" + field }
func idIndexValuesBucketName(col string) string { return idIndexValuesPrefix + col }

func ensureDataBuckets(tx *bolt.Tx, col string) (*bolt.Bucket, error) {
	if _, err := tx.CreateBucketIfNotExists([]byte(indexMetaBucket)); err != nil {
		return nil, err
	}
	if _, err := tx.CreateBucketIfNotExists([]byte(idIndexValuesBucketName(col))); err != nil {
		return nil, err
	}
	b, err := tx.CreateBucketIfNotExists([]byte(dataBucketName(col)))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (c *Collection) withView(fn func(tx *bolt.Tx) error) error {
	if c.tx != nil {
		return fn(c.tx)
	}
	return c.store.db.View(fn)
}

func (c *Collection) withUpdate(fn func(tx *bolt.Tx) error) error {
	if c.tx != nil {
		return fn(c.tx)
	}
	return c.store.db.Update(fn)
}

func (c *Collection) readIndexDefs(meta *bolt.Bucket) []IndexDef {
	var defs []IndexDef
	if raw := meta.Get([]byte(c.name)); raw != nil {
		_ = json.Unmarshal(raw, &defs)
	}
	return defs
}

func (c *Collection) writeIndexDefs(meta *bolt.Bucket, defs []IndexDef) error {
	buf, err := json.Marshal(defs)
	if err != nil {
		return err
	}
	return meta.Put([]byte(c.name), buf)
}

// updateIndexesForDoc updates all known indexes for the document JSON.
func (c *Collection) updateIndexesForDoc(tx *bolt.Tx, id string, jsonDoc []byte) error {
	meta := tx.Bucket([]byte(indexMetaBucket))
	if meta == nil {
		return nil
	}
	defs := c.readIndexDefs(meta)
	var m map[string]any
	if err := json.Unmarshal(jsonDoc, &m); err != nil {
		return err
	}
	vals := make(map[string]string, len(defs))
	for _, d := range defs {
		if val, ok := m[d.Field]; ok {
			enc, ok := encodeIndexValue(val)
			if !ok {
				continue
			}
			idx := tx.Bucket([]byte(indexBucketName(c.name, d.Field)))
			if idx == nil {
				continue
			}
			prefix := append(append([]byte{}, enc...), 0x00)
			if d.Unique {
				cur := idx.Cursor()
				for k, _ := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cur.Next() {
					if !bytes.HasSuffix(k, []byte(id)) {
						return ErrConflict
					}
				}
			}
			key := appendIndexKey(enc, []byte(id))
			if err := idx.Put(key, nil); err != nil {
				return err
			}
			vals[d.Field] = base64.StdEncoding.EncodeToString(enc)
		}
	}
	idb := tx.Bucket([]byte(idIndexValuesBucketName(c.name)))
	if idb == nil {
		return nil
	}
	buf, _ := json.Marshal(vals)
	return idb.Put([]byte(id), buf)
}

// removeOldIndexEntries removes previous index entries for the id based on stored values.
func (c *Collection) removeOldIndexEntries(tx *bolt.Tx, id string) error {
	idb := tx.Bucket([]byte(idIndexValuesBucketName(c.name)))
	if idb == nil {
		return nil
	}
	raw := idb.Get([]byte(id))
	if raw == nil {
		return nil
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err != nil {
		return err
	}
	for field, valStr := range flat {
		idx := tx.Bucket([]byte(indexBucketName(c.name, field)))
		if idx == nil {
			continue
		}
		enc, err := base64.StdEncoding.DecodeString(valStr)
		if err != nil {
			continue
		}
		key := appendIndexKey(enc, []byte(id))
		_ = idx.Delete(key)
	}
	return idb.Delete([]byte(id))
}

// lookupIDsByIndex fills ids matched by field=value using the index. limit<=0 means all.
func (c *Collection) lookupIDsByIndex(field string, value any, ids *[]string, limit int) error {
	return c.withView(func(tx *bolt.Tx) error {
		idx := tx.Bucket([]byte(indexBucketName(c.name, field)))
		if idx == nil {
			return ErrNotFound
		}
		enc, ok := encodeIndexValue(value)
		if !ok {
			return ErrNotFound
		}
		prefix := append(append([]byte{}, enc...), 0x00)
		cur := idx.Cursor()
		for k, _ := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = cur.Next() {
			id := string(k[len(prefix):])
			*ids = append(*ids, id)
			if limit > 0 && len(*ids) >= limit {
				break
			}
		}
		if len(*ids) == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// index key: encodedValue + 0x00 + id
func appendIndexKey(valJSON []byte, id []byte) []byte {
	key := make([]byte, 0, len(valJSON)+1+len(id))
	key = append(key, valJSON...)
	key = append(key, 0x00)
	key = append(key, id...)
	return key
}
