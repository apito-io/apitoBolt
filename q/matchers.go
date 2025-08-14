package q

import (
	"regexp"
	"slices"
	"strconv"
)

// Matcher is a predicate over a JSON-like map[string]any (values from encoding/json).
type Matcher interface{ Match(map[string]any) bool }

type matcherFunc func(map[string]any) bool

func (f matcherFunc) Match(m map[string]any) bool { return f(m) }

func getNum(m map[string]any, field string) (float64, bool) {
	v, ok := m[field]
	if !ok {
		return 0, false
	}
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
	}
	if s, ok := v.(string); ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func getStr(m map[string]any, field string) (string, bool) {
	v, ok := m[field]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func Eq(field string, value any) Matcher {
	return matcherFunc(func(m map[string]any) bool { return equals(m[field], value) })
}
func StrictEq(field string, value any) Matcher {
	return matcherFunc(func(m map[string]any) bool { return m[field] == value })
}
func Gt(field string, value any) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		n, ok := getNum(m, field)
		if !ok {
			return false
		}
		vn, ok := anyToFloat(value)
		if !ok {
			return false
		}
		return n > vn
	})
}
func Gte(field string, value any) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		n, ok := getNum(m, field)
		if !ok {
			return false
		}
		vn, ok := anyToFloat(value)
		if !ok {
			return false
		}
		return n >= vn
	})
}
func Lt(field string, value any) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		n, ok := getNum(m, field)
		if !ok {
			return false
		}
		vn, ok := anyToFloat(value)
		if !ok {
			return false
		}
		return n < vn
	})
}
func Lte(field string, value any) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		n, ok := getNum(m, field)
		if !ok {
			return false
		}
		vn, ok := anyToFloat(value)
		if !ok {
			return false
		}
		return n <= vn
	})
}
func Re(field, pattern string) Matcher {
	re := regexp.MustCompile(pattern)
	return matcherFunc(func(m map[string]any) bool {
		s, ok := getStr(m, field)
		if !ok {
			return false
		}
		return re.MatchString(s)
	})
}
func In(field string, values []string) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		s, ok := getStr(m, field)
		if !ok {
			return false
		}
		return slices.Contains(values, s)
	})
}

func EqF(a, b string) Matcher {
	return matcherFunc(func(m map[string]any) bool { return equals(m[a], m[b]) })
}
func LtF(a, b string) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		av, ok := anyToFloat(m[a])
		if !ok {
			return false
		}
		bv, ok := anyToFloat(m[b])
		if !ok {
			return false
		}
		return av < bv
	})
}
func GtF(a, b string) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		av, ok := anyToFloat(m[a])
		if !ok {
			return false
		}
		bv, ok := anyToFloat(m[b])
		if !ok {
			return false
		}
		return av > bv
	})
}
func LteF(a, b string) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		av, ok := anyToFloat(m[a])
		if !ok {
			return false
		}
		bv, ok := anyToFloat(m[b])
		if !ok {
			return false
		}
		return av <= bv
	})
}
func GteF(a, b string) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		av, ok := anyToFloat(m[a])
		if !ok {
			return false
		}
		bv, ok := anyToFloat(m[b])
		if !ok {
			return false
		}
		return av >= bv
	})
}

func And(ms ...Matcher) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		for _, mm := range ms {
			if !mm.Match(m) {
				return false
			}
		}
		return true
	})
}
func Or(ms ...Matcher) Matcher {
	return matcherFunc(func(m map[string]any) bool {
		for _, mm := range ms {
			if mm.Match(m) {
				return true
			}
		}
		return false
	})
}
func Not(mm Matcher) Matcher { return matcherFunc(func(m map[string]any) bool { return !mm.Match(m) }) }

func anyToFloat(v any) (float64, bool) {
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
	if s, ok := v.(string); ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func equals(a, b any) bool {
	switch av := a.(type) {
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
	}
	if af, ok := anyToFloat(a); ok {
		if bf, ok := anyToFloat(b); ok {
			return af == bf
		}
	}
	return a == b
}
