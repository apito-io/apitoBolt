package apitobolt

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type User struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
	Age    int    `json:"age"`
}

type Tagged struct {
	BoltID string `json:"id" bolt:"id"`
	Name   string `json:"name"`
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestOpenClose(t *testing.T) {
	s := openTestStore(t)
	_ = s.Close()
}

func TestSaveAndFindByID_AutoID_WithTag(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("name", false)

	obj := &Tagged{Name: "alice"}
	id, err := col.Save(obj)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == "" || obj.BoltID == "" || id != obj.BoltID {
		t.Fatalf("id not set correctly: id=%q obj=%+v", id, obj)
	}
	var out Tagged
	if err := col.FindByID(id, &out); err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if out.BoltID != id || out.Name != "alice" {
		t.Fatalf("loaded mismatch: %+v", out)
	}
}

func TestEnsureIndex_UniqueEnforced(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	if err := col.EnsureIndex("email", true); err != nil {
		t.Fatalf("ensure index: %v", err)
	}
	_, err := col.Save(&User{Email: "a@x.com", Name: "A"})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	_, err = col.Save(&User{Email: "a@x.com", Name: "B"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestNonUniqueIndex_AllowsMultipleAndFindAll(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("role", false)
	for i := 0; i < 3; i++ {
		_, err := col.Save(&User{Email: fmt.Sprintf("u%c@x.com", 'a'+i), Role: "user"})
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	_, _ = col.Save(&User{Email: "admin@x.com", Role: "admin"})
	var users []User
	if err := col.FindAll(map[string]any{"role": "user"}, &users, 0, 0); err != nil {
		t.Fatalf("findAll: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}
	var one User
	if err := col.FindOne("role", "user", &one); err != nil {
		t.Fatalf("findOne: %v", err)
	}
	if one.Role != "user" {
		t.Fatalf("unexpected role: %+v", one)
	}
}

func TestUpdate_ReindexCleanup(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("email", true)
	uid, err := col.Save(&User{Email: "a@x.com", Name: "A"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// update email
	var u User
	if err := col.FindByID(uid, &u); err != nil {
		t.Fatalf("get: %v", err)
	}
	u.Email = "b@x.com"
	if _, err := col.Save(&u); err != nil {
		t.Fatalf("update: %v", err)
	}
	var out User
	if err := col.FindOne("email", "b@x.com", &out); err != nil {
		t.Fatalf("find new email: %v", err)
	}
	if out.ID != uid {
		t.Fatalf("id changed: %s vs %s", out.ID, uid)
	}
	// old email should not be found
	var missing User
	err = col.FindOne("email", "a@x.com", &missing)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found for old email, got %v", err)
	}
}

func TestDelete_CleansIndexes(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("email", true)
	id, err := col.Save(&User{Email: "c@x.com", Name: "C"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := col.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var u User
	if err := col.FindByID(id, &u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	if err := col.FindOne("email", "c@x.com", &u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found by index after delete, got %v", err)
	}
}

func TestFindAll_FilterANDWithIndexes(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("role", false)
	_ = col.EnsureIndex("active", false)
	_ = col.EnsureIndex("age", false)
	// data
	_, _ = col.Save(&User{Email: "1@x", Role: "user", Active: true, Age: 30})
	_, _ = col.Save(&User{Email: "2@x", Role: "user", Active: true, Age: 25})
	_, _ = col.Save(&User{Email: "3@x", Role: "user", Active: false, Age: 30})
	_, _ = col.Save(&User{Email: "4@x", Role: "admin", Active: true, Age: 30})
	var got []User
	if err := col.FindAll(map[string]any{"role": "user", "active": true, "age": 30}, &got, 0, 0); err != nil {
		t.Fatalf("findAll: %v", err)
	}
	if len(got) != 1 || got[0].Email != "1@x" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestFindAll_FallbackScan_NoIndex(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_, _ = col.Save(&User{Email: "1@x", Role: "user", Active: true})
	_, _ = col.Save(&User{Email: "2@x", Role: "user", Active: false})
	var got []User
	if err := col.FindAll(map[string]any{"active": false}, &got, 0, 0); err != nil {
		t.Fatalf("findAll: %v", err)
	}
	if len(got) != 1 || got[0].Email != "2@x" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestPagination_LimitOffset(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("role", false)
	for i := 0; i < 5; i++ {
		_, _ = col.Save(&User{Email: fmt.Sprintf("p%c@x", 'a'+i), Role: "user"})
	}
	var got []User
	if err := col.FindAll(map[string]any{"role": "user"}, &got, 2, 1); err != nil {
		t.Fatalf("findAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
}

func TestStormAPI_All_AllByIndex_Find_Range_Prefix_Options(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("age", false)
	_ = col.EnsureIndex("name", false)
	// seed
	_, _ = col.Save(&User{Email: "u1@x", Name: "John", Age: 20})
	_, _ = col.Save(&User{Email: "u2@x", Name: "Joey", Age: 25})
	_, _ = col.Save(&User{Email: "u3@x", Name: "Jane", Age: 30})
	_, _ = col.Save(&User{Email: "u4@x", Name: "Josh", Age: 21})

	// All
	var all []User
	if err := col.All(&all); err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("all expected 4, got %d", len(all))
	}

	// All with options (limit/skip)
	var page []User
	if err := col.All(&page, Limit(2), Skip(1)); err != nil {
		t.Fatalf("all paged: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("all paged expected 2, got %d", len(page))
	}

	// AllByIndex age
	var byAge []User
	if err := col.AllByIndex("age", &byAge); err != nil {
		t.Fatalf("allByIndex: %v", err)
	}
	if !(byAge[0].Age <= byAge[1].Age && byAge[1].Age <= byAge[2].Age) {
		t.Fatalf("age not sorted: %+v", byAge)
	}

	// Find role== (using name prefix later) and options
	var findByName []User
	if err := col.Find("name", "John", &findByName, Reverse()); err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(findByName) != 1 || findByName[0].Name != "John" {
		t.Fatalf("find mismatch: %+v", findByName)
	}

	// Range age [21, 30]
	var ranged []User
	if err := col.Range("age", 21, 30, &ranged); err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(ranged) != 3 {
		t.Fatalf("range expected 3, got %d", len(ranged))
	}

	// Prefix name starting with "Jo"
	var pref []User
	if err := col.Prefix("name", "Jo", &pref); err != nil {
		t.Fatalf("prefix: %v", err)
	}
	if len(pref) < 2 {
		t.Fatalf("prefix expected at least 2, got %d", len(pref))
	}
}

func TestStormAPI_UpdateField_ZeroValue_And_DeleteStruct_Init_Drop_ReIndex(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.Init()
	_ = col.EnsureIndex("age", false)
	id, _ := col.Save(&User{Email: "zero@x", Name: "Zero", Age: 10})

	// zero-value update
	if err := col.UpdateField(&User{ID: id}, "age", 0); err != nil {
		t.Fatalf("update field: %v", err)
	}
	var u User
	if err := col.FindByID(id, &u); err != nil {
		t.Fatalf("get: %v", err)
	}
	if u.Age != 0 {
		t.Fatalf("expected age 0, got %d", u.Age)
	}

	// delete via struct
	if err := col.DeleteStruct(&User{ID: id}); err != nil {
		t.Fatalf("delete struct: %v", err)
	}
	if err := col.FindByID(id, &u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}

	// drop collection
	if err := col.Drop(); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := col.FindByID(id, &u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found after drop, got %v", err)
	}

	// rebuild and reindex
	_ = col.Init()
	_ = col.EnsureIndex("age", false)
	id2, _ := col.Save(&User{Email: "again@x", Name: "Again", Age: 40})
	if err := col.ReIndex(); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	var found User
	if err := col.FindOne("age", 40, &found); err != nil || found.ID != id2 {
		t.Fatalf("find after reindex failed: %v %+v", err, found)
	}
}

func TestStormAPI_Begin_Commit_Rollback(t *testing.T) {
	s := openTestStore(t)
	// rollback
	tx, err := s.Begin(true)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	col := tx.Collection("users")
	_ = col.EnsureIndex("email", true)
	_, _ = col.Save(&User{Email: "tx-rollback@x", Name: "R"})
	_ = tx.Rollback()
	var u User
	if err := s.Collection("users").FindOne("email", "tx-rollback@x", &u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rollback not persisted")
	}

	// commit
	tx2, err := s.Begin(true)
	if err != nil {
		t.Fatalf("begin2: %v", err)
	}
	col2 := tx2.Collection("users")
	_ = col2.EnsureIndex("email", true)
	_, _ = col2.Save(&User{Email: "tx-commit@x", Name: "C"})
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := s.Collection("users").FindOne("email", "tx-commit@x", &u); err != nil {
		t.Fatalf("expected commit persisted, got %v", err)
	}
}

func TestTransactions_ACID(t *testing.T) {
	s := openTestStore(t)
	// rollback case
	_ = s.Update(func(tx *Tx) error {
		col := tx.Collection("users")
		_ = col.EnsureIndex("email", true)
		_, err := col.Save(&User{Email: "rollback@x", Name: "R"})
		if err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	var u User
	if err := s.Collection("users").FindOne("email", "rollback@x", &u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rollback not persisted, got %v", err)
	}
	// commit case
	if err := s.Update(func(tx *Tx) error {
		col := tx.Collection("users")
		_ = col.EnsureIndex("email", true)
		_, err := col.Save(&User{Email: "commit@x", Name: "C"})
		return err
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := s.Collection("users").FindOne("email", "commit@x", &u); err != nil {
		t.Fatalf("expected commit persisted, got %v", err)
	}
}

func TestIDFieldDetection_Variants(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	// ID
	u1 := &struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{Name: "A"}
	id1, err := col.Save(u1)
	if err != nil || id1 == "" || u1.ID != id1 {
		t.Fatalf("ID variant failed: %v %+v", err, u1)
	}
	// Id
	u2 := &struct {
		Id   string `json:"id"`
		Name string `json:"name"`
	}{Name: "B"}
	id2, err := col.Save(u2)
	if err != nil || id2 == "" || u2.Id != id2 {
		t.Fatalf("Id variant failed: %v %+v", err, u2)
	}
	// bolt tag
	u3 := &struct {
		X    string `json:"id" bolt:"id"`
		Name string `json:"name"`
	}{Name: "C"}
	id3, err := col.Save(u3)
	if err != nil || id3 == "" || u3.X != id3 {
		t.Fatalf("bolt tag variant failed: %v %+v", err, u3)
	}
}

func TestJSONEquality_IntAndString(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_ = col.EnsureIndex("age", false)
	_, _ = col.Save(&User{Email: "age30@x", Age: 30})
	var out User
	if err := col.FindOne("age", 30, &out); err != nil {
		t.Fatalf("find age=30: %v", err)
	}
}

func TestLegacyAPI_Compatibility(t *testing.T) {
	s := openTestStore(t)
	// Save via legacy API with explicit id and index field
	u := &User{ID: "123", Email: "legacy@x", Name: "L"}
	if err := s.Save("legacy", u.ID, u, "email"); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
	var out User
	if err := s.Get("legacy", "123", &out); err != nil {
		t.Fatalf("legacy get: %v", err)
	}
	if out.Email != "legacy@x" {
		t.Fatalf("unexpected out: %+v", out)
	}
	var found User
	if err := s.FindOneBy("legacy", "email", "legacy@x", &found); err != nil {
		t.Fatalf("legacy findone: %v", err)
	}
	// Query raw JSON
	var list []User
	if err := s.Query("legacy", func(b []byte) bool {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m["name"] == "L"
	}, &list); err != nil {
		t.Fatalf("legacy query: %v", err)
	}
	if len(list) != 1 || list[0].Email != "legacy@x" {
		t.Fatalf("unexpected query result: %+v", list)
	}
}

func TestQueryFn_Predicate(t *testing.T) {
	s := openTestStore(t)
	col := s.Collection("users")
	_, _ = col.Save(&User{Email: "x1@x", Name: "Ann"})
	_, _ = col.Save(&User{Email: "x2@x", Name: "Bob"})
	var got []User
	if err := col.QueryFn(func(id string, b []byte) bool {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		name, _ := m["name"].(string)
		return strings.HasPrefix(name, "A")
	}, &got); err != nil {
		t.Fatalf("queryfn: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Ann" {
		t.Fatalf("unexpected predicate result: %+v", got)
	}
}
