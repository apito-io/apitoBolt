package apitobolt

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// FindOne finds a single document by equality on a field.
func (c *Collection) FindOne(field string, value any, out any) error {
	res := make([]string, 0, 1)
	if err := c.lookupIDsByIndex(field, value, &res, 1); err == nil && len(res) > 0 {
		return c.FindByID(res[0], out)
	}
	return c.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return ErrNotFound
		}
		var matched []byte
		_ = b.ForEach(func(k, v []byte) error {
			if matched != nil {
				return nil
			}
			var m map[string]any
			if err := json.Unmarshal(v, &m); err == nil {
				if equalsJSONValue(m[field], value) {
					matched = append([]byte{}, v...)
				}
			}
			return nil
		})
		if matched == nil {
			return ErrNotFound
		}
		return json.Unmarshal(matched, out)
	})
}

// One alias of FindOne for Storm-like API.
func (c *Collection) One(field string, value any, out any) error { return c.FindOne(field, value, out) }

// list options and helpers
type listOptions struct {
	limit, skip int
	reverse     bool
}
type Option func(*listOptions)

func Limit(n int) Option { return func(o *listOptions) { o.limit = n } }
func Skip(n int) Option  { return func(o *listOptions) { o.skip = n } }
func Reverse() Option    { return func(o *listOptions) { o.reverse = true } }

// All fetches all documents with optional limit/skip/reverse.
func (c *Collection) All(result any, opts ...Option) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	var o listOptions
	for _, opt := range opts {
		opt(&o)
	}
	return c.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		var ids []string
		_ = b.ForEach(func(k, _ []byte) error { ids = append(ids, string(k)); return nil })
		sort.Strings(ids)
		if o.reverse {
			reverseStrings(ids)
		}
		added := 0
		for _, id := range ids {
			if o.skip > 0 {
				o.skip--
				continue
			}
			if o.limit > 0 && added >= o.limit {
				break
			}
			if v := b.Get([]byte(id)); v != nil {
				elemType := resVal.Elem().Type().Elem()
				newElem := reflect.New(elemType).Interface()
				if err := json.Unmarshal(v, newElem); err == nil {
					resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
					added++
				}
			}
		}
		return nil
	})
}

// AllByIndex returns all documents ordered by the given index field.
func (c *Collection) AllByIndex(field string, result any, opts ...Option) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	var o listOptions
	for _, opt := range opts {
		opt(&o)
	}
	return c.withView(func(tx *bolt.Tx) error {
		idx := tx.Bucket([]byte(indexBucketName(c.name, field)))
		if idx == nil {
			return ErrNotFound
		}
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		cur := idx.Cursor()
		type pair struct {
			id  string
			raw []byte
		}
		var rows []pair
		for k, _ := cur.First(); k != nil; k, _ = cur.Next() {
			ix := bytes.LastIndexByte(k, 0x00)
			if ix <= 0 || ix+1 >= len(k) {
				continue
			}
			id := string(k[ix+1:])
			if v := b.Get([]byte(id)); v != nil {
				rows = append(rows, pair{id: id, raw: append([]byte{}, v...)})
			}
		}
		if o.reverse {
			reversePairs(rows)
		}
		added := 0
		for _, p := range rows {
			if o.skip > 0 {
				o.skip--
				continue
			}
			if o.limit > 0 && added >= o.limit {
				break
			}
			elemType := resVal.Elem().Type().Elem()
			newElem := reflect.New(elemType).Interface()
			if err := json.Unmarshal(p.raw, newElem); err == nil {
				resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
				added++
			}
		}
		return nil
	})
}

// FindAll performs AND-equality filtering using indexes when available.
// filter is a map of field -> value. If filter is empty, returns all.
func (c *Collection) FindAll(filter map[string]any, result any, limit, offset int) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	ids, err := c.planIDs(filter)
	if err != nil {
		return err
	}
	return c.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		appendDoc := func(v []byte) {
			elemType := resVal.Elem().Type().Elem()
			newElem := reflect.New(elemType).Interface()
			if err := json.Unmarshal(v, newElem); err == nil {
				resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
			}
		}
		added := 0
		if ids != nil {
			for i := 0; i < len(ids); i++ {
				if offset > 0 {
					offset--
					continue
				}
				if limit > 0 && added >= limit {
					break
				}
				if v := b.Get([]byte(ids[i])); v != nil {
					if matchJSONFilter(v, filter) {
						appendDoc(v)
						added++
					}
				}
			}
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			if !matchJSONFilter(v, filter) {
				return nil
			}
			if offset > 0 {
				offset--
				return nil
			}
			if limit > 0 && added >= limit {
				return nil
			}
			appendDoc(v)
			added++
			return nil
		})
	})
}

// QueryFn allows a custom predicate over raw JSON bytes.
func (c *Collection) QueryFn(predicate func(id string, data []byte) bool, result any) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	return c.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			if !predicate(string(k), v) {
				return nil
			}
			elemType := resVal.Elem().Type().Elem()
			newElem := reflect.New(elemType).Interface()
			if err := json.Unmarshal(v, newElem); err == nil {
				resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
			}
			return nil
		})
	})
}

// planIDs returns a candidate ordered id list using indexes when possible.
func (c *Collection) planIDs(filter map[string]any) ([]string, error) {
	if len(filter) == 0 {
		return nil, nil
	}
	fields := make([]string, 0, len(filter))
	for f := range filter {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	var idSets [][]string
	for _, f := range fields {
		var ids []string
		if err := c.lookupIDsByIndex(f, filter[f], &ids, 0); err != nil {
			return nil, nil
		}
		idSets = append(idSets, ids)
	}
	if len(idSets) == 0 {
		return nil, nil
	}
	res := intersectSortedStrings(idSets...)
	return res, nil
}

// Find returns all documents where field == value.
func (c *Collection) Find(field string, value any, result any, opts ...Option) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	var o listOptions
	for _, opt := range opts {
		opt(&o)
	}
	var ids []string
	if err := c.lookupIDsByIndex(field, value, &ids, 0); err == nil {
		if o.reverse {
			reverseStrings(ids)
		}
		return c.withView(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(dataBucketName(c.name)))
			if b == nil {
				return nil
			}
			added := 0
			for _, id := range ids {
				if o.skip > 0 {
					o.skip--
					continue
				}
				if o.limit > 0 && added >= o.limit {
					break
				}
				if v := b.Get([]byte(id)); v != nil {
					elemType := resVal.Elem().Type().Elem()
					newElem := reflect.New(elemType).Interface()
					if err := json.Unmarshal(v, newElem); err == nil {
						resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
						added++
					}
				}
			}
			return nil
		})
	}
	// fallback scan
	return c.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		type pair struct {
			id  string
			raw []byte
		}
		var rows []pair
		_ = b.ForEach(func(k, v []byte) error {
			if matchJSONFilter(v, map[string]any{field: value}) {
				rows = append(rows, pair{id: string(k), raw: append([]byte{}, v...)})
			}
			return nil
		})
		sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
		if o.reverse {
			reversePairs(rows)
		}
		added := 0
		for _, p := range rows {
			if o.skip > 0 {
				o.skip--
				continue
			}
			if o.limit > 0 && added >= o.limit {
				break
			}
			elemType := resVal.Elem().Type().Elem()
			newElem := reflect.New(elemType).Interface()
			if err := json.Unmarshal(p.raw, newElem); err == nil {
				resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
				added++
			}
		}
		return nil
	})
}

// Range returns documents where field between [from,to] inclusive using index when available.
func (c *Collection) Range(field string, from, to any, result any, opts ...Option) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	var o listOptions
	for _, opt := range opts {
		opt(&o)
	}
	return c.withView(func(tx *bolt.Tx) error {
		idx := tx.Bucket([]byte(indexBucketName(c.name, field)))
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		if idx == nil {
			type pair struct {
				id  string
				raw []byte
			}
			var rows []pair
			_ = b.ForEach(func(k, v []byte) error {
				if matchRange(v, field, from, to) {
					rows = append(rows, pair{id: string(k), raw: append([]byte{}, v...)})
				}
				return nil
			})
			sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
			if o.reverse {
				reversePairs(rows)
			}
			added := 0
			for _, p := range rows {
				if o.skip > 0 {
					o.skip--
					continue
				}
				if o.limit > 0 && added >= o.limit {
					break
				}
				elemType := resVal.Elem().Type().Elem()
				newElem := reflect.New(elemType).Interface()
				if err := json.Unmarshal(p.raw, newElem); err == nil {
					resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
					added++
				}
			}
			return nil
		}
		start, ok1 := encodeIndexValue(from)
		end, ok2 := encodeIndexValue(to)
		if !ok1 || !ok2 {
			return ErrNotFound
		}
		if len(start) == 0 || len(end) == 0 || start[0] != end[0] {
			return ErrNotFound
		}
		lower := append(append([]byte{}, start...), 0x00)
		upper := append(append([]byte{}, end...), 0x00)
		cur := idx.Cursor()
		type pair struct {
			id  string
			raw []byte
		}
		var rows []pair
		for k, _ := cur.Seek(lower); k != nil && bytes.Compare(k[:len(upper)], upper) <= 0; k, _ = cur.Next() {
			ix := bytes.LastIndexByte(k, 0x00)
			if ix <= 0 || ix+1 >= len(k) {
				continue
			}
			id := string(k[ix+1:])
			if v := b.Get([]byte(id)); v != nil {
				rows = append(rows, pair{id: id, raw: append([]byte{}, v...)})
			}
		}
		if o.reverse {
			reversePairs(rows)
		}
		added := 0
		for _, p := range rows {
			if o.skip > 0 {
				o.skip--
				continue
			}
			if o.limit > 0 && added >= o.limit {
				break
			}
			elemType := resVal.Elem().Type().Elem()
			newElem := reflect.New(elemType).Interface()
			if err := json.Unmarshal(p.raw, newElem); err == nil {
				resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
				added++
			}
		}
		return nil
	})
}

// Prefix returns documents where a string index starts with the given prefix.
func (c *Collection) Prefix(field string, prefix string, result any, opts ...Option) error {
	resVal := reflect.ValueOf(result)
	if resVal.Kind() != reflect.Ptr || resVal.Elem().Kind() != reflect.Slice {
		return ErrBadResult
	}
	var o listOptions
	for _, opt := range opts {
		opt(&o)
	}
	return c.withView(func(tx *bolt.Tx) error {
		idx := tx.Bucket([]byte(indexBucketName(c.name, field)))
		b := tx.Bucket([]byte(dataBucketName(c.name)))
		if b == nil {
			return nil
		}
		if idx == nil {
			type pair struct {
				id  string
				raw []byte
			}
			var rows []pair
			_ = b.ForEach(func(k, v []byte) error {
				var m map[string]any
				if json.Unmarshal(v, &m) == nil {
					if s, ok := m[field].(string); ok && strings.HasPrefix(s, prefix) {
						rows = append(rows, pair{id: string(k), raw: append([]byte{}, v...)})
					}
				}
				return nil
			})
			sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
			if o.reverse {
				reversePairs(rows)
			}
			added := 0
			for _, p := range rows {
				if o.skip > 0 {
					o.skip--
					continue
				}
				if o.limit > 0 && added >= o.limit {
					break
				}
				elemType := resVal.Elem().Type().Elem()
				newElem := reflect.New(elemType).Interface()
				if err := json.Unmarshal(p.raw, newElem); err == nil {
					resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
					added++
				}
			}
			return nil
		}
		valEnc := []byte{'s'}
		valEnc = append(valEnc, []byte(prefix)...)
		cur := idx.Cursor()
		type pair struct {
			id  string
			raw []byte
		}
		var rows []pair
		for k, _ := cur.Seek(valEnc); k != nil && bytes.HasPrefix(k, valEnc); k, _ = cur.Next() {
			ix := bytes.LastIndexByte(k, 0x00)
			if ix <= 0 || ix+1 >= len(k) {
				continue
			}
			id := string(k[ix+1:])
			if v := b.Get([]byte(id)); v != nil {
				rows = append(rows, pair{id: id, raw: append([]byte{}, v...)})
			}
		}
		if o.reverse {
			reversePairs(rows)
		}
		added := 0
		for _, p := range rows {
			if o.skip > 0 {
				o.skip--
				continue
			}
			if o.limit > 0 && added >= o.limit {
				break
			}
			elemType := resVal.Elem().Type().Elem()
			newElem := reflect.New(elemType).Interface()
			if err := json.Unmarshal(p.raw, newElem); err == nil {
				resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
				added++
			}
		}
		return nil
	})
}

// Store-level Storm-like helpers
func (s *Store) One(collection, field string, value any, out any) error {
	return s.Collection(collection).One(field, value, out)
}
func (s *Store) Find(collection, field string, value any, result any, opts ...Option) error {
	return s.Collection(collection).Find(field, value, result, opts...)
}
func (s *Store) All(collection string, result any, opts ...Option) error {
	return s.Collection(collection).All(result, opts...)
}
func (s *Store) AllByIndex(collection, field string, result any, opts ...Option) error {
	return s.Collection(collection).AllByIndex(field, result, opts...)
}
func (s *Store) Range(collection, field string, from, to any, result any, opts ...Option) error {
	return s.Collection(collection).Range(field, from, to, result, opts...)
}
func (s *Store) Prefix(collection, field, prefix string, result any, opts ...Option) error {
	return s.Collection(collection).Prefix(field, prefix, result, opts...)
}
