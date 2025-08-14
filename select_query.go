package apitobolt

import (
	"encoding/json"
	"reflect"
	"sort"

	bolt "go.etcd.io/bbolt"
)

type Query struct {
	col     *Collection
	ms      []qMatcher
	limit   int
	skip    int
	reverse bool
	order   []orderSpec
}

// lightweight internal adapter to avoid importing q in main package surface
type qMatcher interface{ Match(map[string]any) bool }

// Select builds a query with matchers. The matcher interface is satisfied by types in the subpackage q.
func (c *Collection) Select(matchers ...qMatcher) *Query { return &Query{col: c, ms: matchers} }

func (q *Query) Limit(n int) *Query { q.limit = n; return q }
func (q *Query) Skip(n int) *Query  { q.skip = n; return q }
func (q *Query) Reverse() *Query    { q.reverse = true; return q }
func (q *Query) OrderBy(fields ...string) *Query {
	for _, f := range fields {
		q.order = append(q.order, orderSpec{field: f, desc: false})
	}
	return q
}
func (q *Query) OrderByDesc(fields ...string) *Query {
	for _, f := range fields {
		q.order = append(q.order, orderSpec{field: f, desc: true})
	}
	return q
}

func (q *Query) Find(result any) error {
	return q.col.withView(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(q.col.name)))
		if b == nil {
			return nil
		}
		// gather all matching
		var rows []row
		_ = b.ForEach(func(k, v []byte) error {
			var m map[string]any
			if json.Unmarshal(v, &m) == nil {
				matched := true
				for _, mm := range q.ms {
					if !mm.Match(m) {
						matched = false
						break
					}
				}
				if matched {
					// capture sort keys
					keys := make([]any, len(q.order))
					for i, sp := range q.order {
						keys[i] = m[sp.field]
					}
					rows = append(rows, row{id: string(k), raw: append([]byte{}, v...), sortKeys: keys})
				}
			}
			return nil
		})
		// ordering
		if len(q.order) > 0 {
			sort.Slice(rows, func(i, j int) bool {
				// lexicographic by sortKeys
				for k := 0; k < len(q.order); k++ {
					a, b := rows[i].sortKeys[k], rows[j].sortKeys[k]
					if eqJSONValue(a, b) {
						continue
					}
					if q.order[k].desc {
						return lessJSONValue(b, a)
					}
					return lessJSONValue(a, b)
				}
				return rows[i].id < rows[j].id
			})
		} else {
			sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
		}
		if q.reverse {
			reversePairs(rows)
		}
		// pagination and unmarshal
		add := 0
		return appendRows(result, rowsToRowLike(rows), q.skip, q.limit, &add)
	})
}

func (q *Query) First(out any) error {
	var list []map[string]any
	if err := q.Limit(1).Find(&list); err != nil {
		return err
	}
	if len(list) == 0 {
		return ErrNotFound
	}
	b, _ := json.Marshal(list[0])
	return json.Unmarshal(b, out)
}

func (q *Query) Delete(example any) error {
	// iterate and delete matching records
	return q.col.withUpdate(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dataBucketName(q.col.name)))
		if b == nil {
			return nil
		}
		toDelete := make([]string, 0, 16)
		_ = b.ForEach(func(k, v []byte) error {
			var m map[string]any
			if json.Unmarshal(v, &m) == nil {
				matched := true
				for _, mm := range q.ms {
					if !mm.Match(m) {
						matched = false
						break
					}
				}
				if matched {
					toDelete = append(toDelete, string(k))
				}
			}
			return nil
		})
		for _, id := range toDelete {
			if err := q.col.removeOldIndexEntries(tx, id); err != nil {
				return err
			}
			if err := b.Delete([]byte(id)); err != nil {
				return err
			}
		}
		return nil
	})
}

// helpers
func appendRows(result any, rows []rowLike, skip, limit int, added *int) error {
	resVal := reflectValueOfSlice(result)
	for _, p := range rows {
		if skip > 0 {
			skip--
			continue
		}
		if limit > 0 && *added >= limit {
			break
		}
		elemType := resVal.Elem().Type().Elem()
		newElem := reflect.New(elemType).Interface()
		if err := json.Unmarshal(p.rawBytes(), newElem); err == nil {
			resVal.Elem().Set(reflect.Append(resVal.Elem(), reflect.ValueOf(newElem).Elem()))
			*added++
		}
	}
	return nil
}

type rowLike interface{ rawBytes() []byte }
type row struct {
	id       string
	raw      []byte
	sortKeys []any
}

func (r row) rawBytes() []byte { return r.raw }

type orderSpec struct {
	field string
	desc  bool
}

func rowsToRowLike(in []row) []rowLike {
	out := make([]rowLike, len(in))
	for i := range in {
		out[i] = in[i]
	}
	return out
}

// value comparisons used for OrderBy
func eqJSONValue(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
func lessJSONValue(a, b any) bool {
	// numeric compare if possible
	if af, ok := anyToFloatGeneric(a); ok {
		if bf, ok := anyToFloatGeneric(b); ok {
			return af < bf
		}
	}
	// string compare if possible
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as < bs
	}
	// fallback to marshaled byte compare
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) < string(bb)
}

// minimal reflection helpers to avoid importing reflect everywhere
func reflectValueOfSlice(result any) reflect.Value { return reflect.ValueOf(result) }

// reuse util anyToFloat logic (duplicate to keep package boundary minimal)
func anyToFloatGeneric(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint32:
		return float64(t), true
	}
	return 0, false
}
