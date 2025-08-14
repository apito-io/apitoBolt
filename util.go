package apitobolt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"sort"
)

func reverseStrings(a []string) {
	for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
}

func reversePairs[T any](a []T) {
	for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
}

// encodeIndexValue returns a type-tagged, order-preserving encoding for indexing.
func encodeIndexValue(v any) ([]byte, bool) {
	switch t := v.(type) {
	case nil:
		return []byte{'n'}, true
	case bool:
		if t {
			return []byte{'b', 1}, true
		}
		return []byte{'b', 0}, true
	case string:
		return append([]byte{'s'}, []byte(t)...), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return append([]byte{'f'}, encodeOrderedFloat64(f)...), true
		}
		return nil, false
	case float32:
		return append([]byte{'f'}, encodeOrderedFloat64(float64(t))...), true
	case float64:
		return append([]byte{'f'}, encodeOrderedFloat64(t)...), true
	case int, int8, int16, int32, int64:
		return append([]byte{'f'}, encodeOrderedFloat64(float64(reflect.ValueOf(v).Int()))...), true
	case uint, uint8, uint16, uint32, uint64:
		return append([]byte{'f'}, encodeOrderedFloat64(float64(reflect.ValueOf(v).Uint()))...), true
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Float64, reflect.Float32:
			return append([]byte{'f'}, encodeOrderedFloat64(rv.Convert(reflect.TypeOf(float64(0))).Float())...), true
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return append([]byte{'f'}, encodeOrderedFloat64(float64(rv.Int()))...), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return append([]byte{'f'}, encodeOrderedFloat64(float64(rv.Uint()))...), true
		case reflect.String:
			return append([]byte{'s'}, []byte(rv.String())...), true
		case reflect.Bool:
			if rv.Bool() {
				return []byte{'b', 1}, true
			}
			return []byte{'b', 0}, true
		default:
			return nil, false
		}
	}
}

func encodeSignedInt64(n int64) []byte {
	u := uint64(n) ^ (1 << 63)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, u)
	return buf
}

func encodeUint64(u uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, u)
	return buf
}

func encodeOrderedFloat64(f float64) []byte {
	bits := math.Float64bits(f)
	if bits&(1<<63) != 0 { // negative
		bits = ^bits
	} else {
		bits = bits | (1 << 63)
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, bits)
	return buf
}

func isZeroValue(v reflect.Value) bool {
	return reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}

func intersectSortedStrings(sets ...[]string) []string {
	if len(sets) == 1 {
		return sets[0]
	}
	for i := range sets {
		sort.Strings(sets[i])
	}
	res := sets[0]
	for i := 1; i < len(sets); i++ {
		res = intersectTwoSorted(res, sets[i])
		if len(res) == 0 {
			break
		}
	}
	return res
}

func intersectTwoSorted(a, b []string) []string {
	i, j := 0, 0
	var out []string
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return out
}

func matchJSONFilter(jsonBytes []byte, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	var m map[string]any
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return false
	}
	for k, v := range filter {
		mv, ok := m[k]
		if !ok || !equalsJSONValue(mv, v) {
			return false
		}
	}
	return true
}

func matchRange(jsonBytes []byte, field string, from, to any) bool {
	var m map[string]any
	if json.Unmarshal(jsonBytes, &m) != nil {
		return false
	}
	val, ok := m[field]
	if !ok {
		return false
	}
	start, ok1 := encodeIndexValue(val)
	fromEnc, ok2 := encodeIndexValue(from)
	toEnc, ok3 := encodeIndexValue(to)
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	if start[0] != fromEnc[0] || start[0] != toEnc[0] {
		return false
	}
	s := start[1:]
	f := fromEnc[1:]
	t := toEnc[1:]
	return bytes.Compare(s, f) >= 0 && bytes.Compare(s, t) <= 0
}
