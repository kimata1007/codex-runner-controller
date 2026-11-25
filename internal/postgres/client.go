package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

type Client struct {
	db *sql.DB
}

type Value struct {
	Type  string
	Value string
}

type ExecResult struct {
	Columns          []string
	Rows             [][]Value
	AffectedRowCount int
}

type TxStatement struct {
	SQL  string
	Args []Value
}

func NewClient(ctx context.Context, dsn string) (*Client, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL configuration")
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("PostgreSQL connection failed")
	}
	return &Client{db: db}, nil
}

func (c *Client) Close() error { return c.db.Close() }

func Text(v string) Value { return Value{Type: "text", Value: v} }
func Integer(v int64) Value {
	return Value{Type: "integer", Value: strconv.FormatInt(v, 10)}
}
func Null() Value { return Value{Type: "null"} }

func arguments(values []Value) []any {
	args := make([]any, len(values))
	for i, v := range values {
		if v.Type != "null" {
			args[i] = v.Value
		}
	}
	return args
}

// Serialize writes across API processes, preserving the existing MAX(sequence)+1
// allocation and claim semantics. Reads use the connection pool directly.
func (c *Client) writeTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(746294103)"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) Exec(ctx context.Context, query string, values []Value, wantRows bool) (result ExecResult, err error) {
	args := arguments(values)
	if wantRows {
		rows, err := c.db.QueryContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		result.Columns, err = rows.Columns()
		if err != nil {
			return result, err
		}
		for rows.Next() {
			raw := make([]any, len(result.Columns))
			dest := make([]any, len(raw))
			for i := range raw {
				dest[i] = &raw[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return result, err
			}
			row := make([]Value, len(raw))
			for i, v := range raw {
				switch x := v.(type) {
			case nil:
				row[i] = Null()
			case int64:
				row[i] = Integer(x)
			case []byte:
				row[i] = Text(string(x))
			case time.Time:
				row[i] = Text(x.UTC().Format(time.RFC3339Nano))
			default:
				row[i] = Text(fmt.Sprint(x))
				}
			}
			result.Rows = append(result.Rows, row)
		}
		return result, rows.Err()
	}
	err = c.writeTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		result.AffectedRowCount = int(n)
		return err
	})
	return result, err
}

func (c *Client) ExecTransaction(ctx context.Context, statements []TxStatement) error {
	return c.writeTx(ctx, func(tx *sql.Tx) error {
		for _, s := range statements {
			if _, err := tx.ExecContext(ctx, s.SQL, arguments(s.Args)...); err != nil {
				return err
			}
		}
		return nil
	})
}

func (c *Client) ApplySchema(ctx context.Context, schema string) error {
	return c.writeTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, schema)
		return err
	})
}
