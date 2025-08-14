# ApitoBolt

ApitoBolt is a modern, lightweight Storm-style wrapper around bbolt that provides Mongo-like collections, JSON documents, secondary indexes, ACID CRUD, and simple queries with a tiny footprint.

## Features

- Collection-based API: `store.Collection("users")`
- CRUD for Go structs (automatic JSON marshal/unmarshal)
- Secondary indexes (unique and non-unique)
- Equality queries, prefix and range queries
- Pagination options: Limit, Skip, Reverse
- ACID transactions (`View`, `Update`, and explicit `Begin/Commit/Rollback`)
- Storm-like helpers: `One`, `Find`, `All`, `AllByIndex`, `Range`, `Prefix`
- Update helpers: `Update`, `UpdateField`, `DeleteStruct`, `Init`, `Drop`, `ReIndex`

## Install

```bash
go get github.com/apito-io/apitoBolt
```

## Quick Start

```go
package main

import (
    "fmt"
    bolt "github.com/apito-io/apitoBolt"
)

type User struct {
    ID     string `json:"id"`
    Email  string `json:"email"`
    Name   string `json:"name"`
    Role   string `json:"role"`
    Active bool   `json:"active"`
    Age    int    `json:"age"`
}

func main() {
    store, _ := bolt.Open("app.db")
    defer store.Close()

    users := store.Collection("users")
    _ = users.EnsureIndex("email", true)
    _ = users.EnsureIndex("age", false)

    id, _ := users.Save(&User{Email: "a@x.com", Name: "Alice", Age: 30})

    var u User
    _ = users.FindByID(id, &u)
    fmt.Println("loaded:", u.Email)

    // Storm-style queries
    var list []User
    _ = users.Find("age", 30, &list, bolt.Limit(10))
}
```

## Storm-like API Cheatsheet

- Fetch one

```go
var user User
_ = users.One("Email", "john@x.com", &user)
```

- Fetch many by equality

```go
var users []User
_ = users.Find("Role", "staff", &users, bolt.Skip(10), bolt.Limit(10), bolt.Reverse())
```

- Fetch all / sorted by index

```go
var users []User
_ = users.All(&users, bolt.Limit(10))
_ = users.AllByIndex("CreatedAt", &users, bolt.Reverse())
```

- Range and Prefix

```go
var teens []User
_ = users.Range("Age", 13, 19, &teens)

var jo []User
_ = users.Prefix("Name", "Jo", &jo)
```

- Update helpers

```go
// Non-zero field update
_ = users.Update(&User{ID: "123", Name: "Jack", Age: 45})

// Single field update (zero allowed)
_ = users.UpdateField(&User{ID: "123"}, "Age", 0)

// Delete by struct
_ = users.DeleteStruct(&User{ID: "123"})
```

- Lifecycle

```go
_ = users.Init()    // ensure buckets and index state
_ = users.Drop()    // drop data and indexes
_ = users.ReIndex() // rebuild all indexes
```

- Transactions

```go
// High-level
_ = store.Update(func(tx *bolt.Tx) error {
    col := tx.Collection("users")
    _, err := col.Save(&User{Email: "b@x.com"})
    return err
})

// Explicit
tx, _ := store.Begin(true)
col := tx.Collection("users")
_, _ = col.Save(&User{Email: "c@x.com"})
_ = tx.Commit()
```

## Design Notes

- Index keys are order-preserving and type-tagged for correct range ordering
- Per-document index metadata enables safe update/delete reindexing
- Equality filters AND-combine when all fields are indexed; otherwise fallback scan

## License

MIT (see `LICENSE`)

## Releasing

- Local: `./scripts/release.sh` (requires GoReleaser)
- CI: Tag a commit like `v0.1.0` and push; GitHub Actions will build and draft a release
