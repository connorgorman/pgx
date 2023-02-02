package pgxpool

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/puddle"
)

// Conn is an acquired *pgx.Conn from a Pool.
type Conn struct {
	res *puddle.Resource
	p   *Pool
}

// Release returns c to the pool it was acquired from. Once Release has been called, other methods must not be called.
// However, it is safe to call Release multiple times. Subsequent calls after the first will be ignored.
func (c *Conn) Release() {
	if c.res == nil {
		return
	}

	conn := c.Conn()
	res := c.res
	c.res = nil

	if conn.IsClosed() || conn.PgConn().IsBusy() || conn.PgConn().TxStatus() != 'I' {
		res.Destroy()
		// Signal to the health check to run since we just destroyed a connections
		// and we might be below minConns now
		c.p.triggerHealthCheck()
		return
	}

	// If the pool is consistently being used, we might never get to check the
	// lifetime of a connection since we only check idle connections in checkConnsHealth
	// so we also check the lifetime here and force a health check
	if c.p.isExpired(res) {
		atomic.AddInt64(&c.p.lifetimeDestroyCount, 1)
		res.Destroy()
		// Signal to the health check to run since we just destroyed a connections
		// and we might be below minConns now
		c.p.triggerHealthCheck()
		return
	}

	if c.p.afterRelease == nil {
		res.Release()
		return
	}

	go func() {
		if c.p.afterRelease(conn) {
			res.Release()
		} else {
			res.Destroy()
			// Signal to the health check to run since we just destroyed a connections
			// and we might be below minConns now
			c.p.triggerHealthCheck()
		}
	}()
}

// Hijack assumes ownership of the connection from the pool. Caller is responsible for closing the connection. Hijack
// will panic if called on an already released or hijacked connection.
func (c *Conn) Hijack() *pgx.Conn {
	if c.res == nil {
		panic("cannot hijack already released or hijacked connection")
	}

	conn := c.Conn()
	res := c.res
	c.res = nil

	res.Hijack()

	return conn
}

func deadlineCheck(ctx context.Context, sql string) {
	if _, ok := ctx.Deadline(); !ok {
		fmt.Println("No deadline for query", sql)
	}
}

func (c *Conn) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	deadlineCheck(ctx, sql)
	return c.Conn().Exec(ctx, sql, arguments...)
}

func (c *Conn) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	deadlineCheck(ctx, sql)
	return c.Conn().Query(ctx, sql, args...)
}

func (c *Conn) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	deadlineCheck(ctx, sql)
	return c.Conn().QueryRow(ctx, sql, args...)
}

func (c *Conn) QueryFunc(ctx context.Context, sql string, args []interface{}, scans []interface{}, f func(pgx.QueryFuncRow) error) (pgconn.CommandTag, error) {
	deadlineCheck(ctx, sql)
	return c.Conn().QueryFunc(ctx, sql, args, scans, f)
}

func (c *Conn) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	deadlineCheck(ctx, "some batch query")
	return c.Conn().SendBatch(ctx, b)
}

func (c *Conn) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	deadlineCheck(ctx, "copy from"+tableName.Sanitize())
	return c.Conn().CopyFrom(ctx, tableName, columnNames, rowSrc)
}

// Begin starts a transaction block from the *Conn without explicitly setting a transaction mode (see BeginTx with TxOptions if transaction mode is required).
func (c *Conn) Begin(ctx context.Context) (pgx.Tx, error) {
	deadlineCheck(ctx, "begin call")
	return c.Conn().Begin(ctx)
}

// BeginTx starts a transaction block from the *Conn with txOptions determining the transaction mode.
func (c *Conn) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	deadlineCheck(ctx, "begin tx")
	return c.Conn().BeginTx(ctx, txOptions)
}

func (c *Conn) BeginFunc(ctx context.Context, f func(pgx.Tx) error) error {
	deadlineCheck(ctx, "begin func")
	return c.Conn().BeginFunc(ctx, f)
}

func (c *Conn) BeginTxFunc(ctx context.Context, txOptions pgx.TxOptions, f func(pgx.Tx) error) error {
	deadlineCheck(ctx, "begin tx func")
	return c.Conn().BeginTxFunc(ctx, txOptions, f)
}

func (c *Conn) Ping(ctx context.Context) error {
	deadlineCheck(ctx, "ping")
	return c.Conn().Ping(ctx)
}

func (c *Conn) Conn() *pgx.Conn {
	return c.connResource().conn
}

func (c *Conn) connResource() *connResource {
	return c.res.Value().(*connResource)
}

func (c *Conn) getPoolRow(r pgx.Row) *poolRow {
	return c.connResource().getPoolRow(c, r)
}

func (c *Conn) getPoolRows(r pgx.Rows) *poolRows {
	return c.connResource().getPoolRows(c, r)
}
