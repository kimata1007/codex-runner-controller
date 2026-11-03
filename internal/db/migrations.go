package db

import _ "embed"

// InitialSchema is the first schema applied to the Conversation Data store.
// The SQL is SQLite-compatible and is intended for libSQL/sqld.
//
//go:embed migrations/001_initial.sql
var InitialSchema string
