package apitobolt

import (
    "encoding/json"
    "errors"
    "reflect"
    "strings"
)

// ID helpers: reflect set/get
func extractID(doc any) (string, bool) {
    rv := reflect.ValueOf(doc)
    if rv.Kind() == reflect.Ptr { rv = rv.Elem() }
    if rv.Kind() != reflect.Struct { return "", false }
    if id, ok := findTaggedField(rv, "bolt", "id"); ok { return id, true }
    if f := rv.FieldByName("ID"); f.IsValid() && f.Kind() == reflect.String { return f.String(), true }
    if f := rv.FieldByName("Id"); f.IsValid() && f.Kind() == reflect.String { return f.String(), true }
    if f := rv.FieldByName("id"); f.IsValid() && f.Kind() == reflect.String { return f.String(), true }
    return "", false
}

func setID(doc any, id string) error {
    rv := reflect.ValueOf(doc)
    if rv.Kind() != reflect.Ptr { return errors.New("doc must be pointer to struct") }
    rv = rv.Elem()
    if rv.Kind() != reflect.Struct { return errors.New("doc must be pointer to struct") }
    if setTaggedField(rv, "bolt", "id", id) { return nil }
    if f := rv.FieldByName("ID"); f.IsValid() && f.CanSet() && f.Kind() == reflect.String { f.SetString(id); return nil }
    if f := rv.FieldByName("Id"); f.IsValid() && f.CanSet() && f.Kind() == reflect.String { f.SetString(id); return nil }
    if f := rv.FieldByName("id"); f.IsValid() && f.CanSet() && f.Kind() == reflect.String { f.SetString(id); return nil }
    return nil
}

func findTaggedField(rv reflect.Value, tagKey, tagExpected string) (string, bool) {
    rt := rv.Type()
    for i := 0; i < rv.NumField(); i++ {
        f := rt.Field(i)
        tag := f.Tag.Get(tagKey)
        if tag == "" { continue }
        parts := strings.Split(tag, ",")
        for _, p := range parts {
            if strings.TrimSpace(p) == tagExpected {
                fv := rv.Field(i)
                if fv.Kind() == reflect.String { return fv.String(), true }
            }
        }
    }
    return "", false
}

func setTaggedField(rv reflect.Value, tagKey, tagExpected, value string) bool {
    rt := rv.Type()
    for i := 0; i < rv.NumField(); i++ {
        f := rt.Field(i)
        tag := f.Tag.Get(tagKey)
        if tag == "" { continue }
        parts := strings.Split(tag, ",")
        for _, p := range parts {
            if strings.TrimSpace(p) == tagExpected {
                fv := rv.Field(i)
                if fv.CanSet() && fv.Kind() == reflect.String { fv.SetString(value); return true }
            }
        }
    }
    return false
}

// equalsJSONValue compares values by their JSON encoding (canonical enough for simple equality).
func equalsJSONValue(a any, b any) bool {
    ab, _ := json.Marshal(a)
    bb, _ := json.Marshal(b)
    return string(ab) == string(bb)
}


