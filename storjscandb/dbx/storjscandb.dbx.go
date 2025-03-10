//lint:file-ignore U1000,ST1012 generated file
// AUTOGENERATED BY storj.io/dbx
// DO NOT EDIT.

package dbx

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4/stdlib"

	"storj.io/private/tagsql"
)

// Prevent conditional imports from causing build failures.
var _ = strconv.Itoa
var _ = strings.LastIndex
var _ = fmt.Sprint
var _ sync.Mutex

var (
	WrapErr     = func(err *Error) error { return err }
	Logger      func(format string, args ...interface{})
	ShouldRetry func(driver string, err error) bool

	errTooManyRows       = errors.New("too many rows")
	errUnsupportedDriver = errors.New("unsupported driver")
	errEmptyUpdate       = errors.New("empty update")
)

func logError(format string, args ...interface{}) {
	if Logger != nil {
		Logger(format, args...)
	}
}

type ErrorCode int

const (
	ErrorCode_Unknown ErrorCode = iota
	ErrorCode_UnsupportedDriver
	ErrorCode_NoRows
	ErrorCode_TxDone
	ErrorCode_TooManyRows
	ErrorCode_ConstraintViolation
	ErrorCode_EmptyUpdate
)

type Error struct {
	Err         error
	Code        ErrorCode
	Driver      string
	Constraint  string
	QuerySuffix string
}

func (e *Error) Error() string {
	return e.Err.Error()
}

func wrapErr(e *Error) error {
	if WrapErr == nil {
		return e
	}
	return WrapErr(e)
}

func makeErr(err error) error {
	if err == nil {
		return nil
	}
	e := &Error{Err: err}
	switch err {
	case sql.ErrNoRows:
		e.Code = ErrorCode_NoRows
	case sql.ErrTxDone:
		e.Code = ErrorCode_TxDone
	}
	return wrapErr(e)
}

func shouldRetry(driver string, err error) bool {
	if ShouldRetry == nil {
		return false
	}
	return ShouldRetry(driver, err)
}

func unsupportedDriver(driver string) error {
	return wrapErr(&Error{
		Err:    errUnsupportedDriver,
		Code:   ErrorCode_UnsupportedDriver,
		Driver: driver,
	})
}

func emptyUpdate() error {
	return wrapErr(&Error{
		Err:  errEmptyUpdate,
		Code: ErrorCode_EmptyUpdate,
	})
}

func tooManyRows(query_suffix string) error {
	return wrapErr(&Error{
		Err:         errTooManyRows,
		Code:        ErrorCode_TooManyRows,
		QuerySuffix: query_suffix,
	})
}

func constraintViolation(err error, constraint string) error {
	return wrapErr(&Error{
		Err:        err,
		Code:       ErrorCode_ConstraintViolation,
		Constraint: constraint,
	})
}

type driver interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (tagsql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

var (
	notAPointer     = errors.New("destination not a pointer")
	lossyConversion = errors.New("lossy conversion")
)

type DB struct {
	tagsql.DB
	dbMethods

	Hooks struct {
		Now func() time.Time
	}

	driver string
}

func Open(driver, source string) (db *DB, err error) {
	var sql_db *sql.DB
	switch driver {
	case "pgx":
		sql_db, err = openpgx(source)
	default:
		return nil, unsupportedDriver(driver)
	}
	if err != nil {
		return nil, makeErr(err)
	}
	defer func(sql_db *sql.DB) {
		if err != nil {
			sql_db.Close()
		}
	}(sql_db)

	if err := sql_db.Ping(); err != nil {
		return nil, makeErr(err)
	}

	db = &DB{
		DB: tagsql.Wrap(sql_db),

		driver: driver,
	}
	db.Hooks.Now = time.Now

	switch driver {
	case "pgx":
		db.dbMethods = newpgx(db)
	default:
		return nil, unsupportedDriver(driver)
	}

	return db, nil
}

func (obj *DB) Close() (err error) {
	return obj.makeErr(obj.DB.Close())
}

func (obj *DB) Open(ctx context.Context) (*Tx, error) {
	tx, err := obj.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, obj.makeErr(err)
	}

	return &Tx{
		Tx:        tx,
		txMethods: obj.wrapTx(tx),
	}, nil
}

func (obj *DB) NewRx() *Rx {
	return &Rx{db: obj}
}

func DeleteAll(ctx context.Context, db *DB) (int64, error) {
	tx, err := db.Open(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err == nil {
			err = db.makeErr(tx.Commit())
			return
		}

		if err_rollback := tx.Rollback(); err_rollback != nil {
			logError("delete-all: rollback failed: %v", db.makeErr(err_rollback))
		}
	}()
	return tx.deleteAll(ctx)
}

type Tx struct {
	Tx tagsql.Tx
	txMethods
}

type dialectTx struct {
	tx tagsql.Tx
}

func (tx *dialectTx) Commit() (err error) {
	return makeErr(tx.tx.Commit())
}

func (tx *dialectTx) Rollback() (err error) {
	return makeErr(tx.tx.Rollback())
}

type pgxImpl struct {
	db      *DB
	dialect __sqlbundle_pgx
	driver  driver
	txn     bool
}

func (obj *pgxImpl) Rebind(s string) string {
	return obj.dialect.Rebind(s)
}

func (obj *pgxImpl) logStmt(stmt string, args ...interface{}) {
	pgxLogStmt(stmt, args...)
}

func (obj *pgxImpl) makeErr(err error) error {
	constraint, ok := obj.isConstraintError(err)
	if ok {
		return constraintViolation(err, constraint)
	}
	return makeErr(err)
}

func (obj *pgxImpl) shouldRetry(err error) bool {
	return !obj.txn && shouldRetry(obj.db.driver, err)
}

type pgxImpl_retryingRow struct {
	obj   *pgxImpl
	ctx   context.Context
	query string
	args  []interface{}
}

func (obj *pgxImpl) queryRowContext(ctx context.Context, query string, args ...interface{}) *pgxImpl_retryingRow {
	return &pgxImpl_retryingRow{
		obj:   obj,
		ctx:   ctx,
		query: query,
		args:  args,
	}
}

func (rows *pgxImpl_retryingRow) Scan(dest ...interface{}) error {
	for {
		err := rows.obj.driver.QueryRowContext(rows.ctx, rows.query, rows.args...).Scan(dest...)
		if err != nil {
			if rows.obj.shouldRetry(err) {
				continue
			}
			// caller will wrap this error
			return err
		}
		return nil
	}
}

type pgxDB struct {
	db *DB
	*pgxImpl
}

func newpgx(db *DB) *pgxDB {
	return &pgxDB{
		db: db,
		pgxImpl: &pgxImpl{
			db:     db,
			driver: db.DB,
		},
	}
}

func (obj *pgxDB) Schema() string {
	return `CREATE TABLE block_headers (
	hash bytea NOT NULL,
	number bigint NOT NULL,
	timestamp timestamp with time zone NOT NULL,
	created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
	PRIMARY KEY ( hash )
);
CREATE INDEX block_header_timestamp ON block_headers ( timestamp ) ;`
}

func (obj *pgxDB) wrapTx(tx tagsql.Tx) txMethods {
	return &pgxTx{
		dialectTx: dialectTx{tx: tx},
		pgxImpl: &pgxImpl{
			db:     obj.db,
			driver: tx,
			txn:    true,
		},
	}
}

type pgxTx struct {
	dialectTx
	*pgxImpl
}

func pgxLogStmt(stmt string, args ...interface{}) {
	// TODO: render placeholders
	if Logger != nil {
		out := fmt.Sprintf("stmt: %s\nargs: %v\n", stmt, pretty(args))
		Logger(out)
	}
}

type pretty []interface{}

func (p pretty) Format(f fmt.State, c rune) {
	fmt.Fprint(f, "[")
nextval:
	for i, val := range p {
		if i > 0 {
			fmt.Fprint(f, ", ")
		}
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				fmt.Fprint(f, "NULL")
				continue
			}
			val = rv.Elem().Interface()
		}
		switch v := val.(type) {
		case string:
			fmt.Fprintf(f, "%q", v)
		case time.Time:
			fmt.Fprintf(f, "%s", v.Format(time.RFC3339Nano))
		case []byte:
			for _, b := range v {
				if !unicode.IsPrint(rune(b)) {
					fmt.Fprintf(f, "%#x", v)
					continue nextval
				}
			}
			fmt.Fprintf(f, "%q", v)
		default:
			fmt.Fprintf(f, "%v", v)
		}
	}
	fmt.Fprint(f, "]")
}

type BlockHeader struct {
	Hash      []byte
	Number    int64
	Timestamp time.Time
	CreatedAt time.Time
}

func (BlockHeader) _Table() string { return "block_headers" }

type BlockHeader_Update_Fields struct {
}

type BlockHeader_Hash_Field struct {
	_set   bool
	_null  bool
	_value []byte
}

func BlockHeader_Hash(v []byte) BlockHeader_Hash_Field {
	return BlockHeader_Hash_Field{_set: true, _value: v}
}

func (f BlockHeader_Hash_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (BlockHeader_Hash_Field) _Column() string { return "hash" }

type BlockHeader_Number_Field struct {
	_set   bool
	_null  bool
	_value int64
}

func BlockHeader_Number(v int64) BlockHeader_Number_Field {
	return BlockHeader_Number_Field{_set: true, _value: v}
}

func (f BlockHeader_Number_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (BlockHeader_Number_Field) _Column() string { return "number" }

type BlockHeader_Timestamp_Field struct {
	_set   bool
	_null  bool
	_value time.Time
}

func BlockHeader_Timestamp(v time.Time) BlockHeader_Timestamp_Field {
	return BlockHeader_Timestamp_Field{_set: true, _value: v}
}

func (f BlockHeader_Timestamp_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (BlockHeader_Timestamp_Field) _Column() string { return "timestamp" }

type BlockHeader_CreatedAt_Field struct {
	_set   bool
	_null  bool
	_value time.Time
}

func BlockHeader_CreatedAt(v time.Time) BlockHeader_CreatedAt_Field {
	return BlockHeader_CreatedAt_Field{_set: true, _value: v}
}

func (f BlockHeader_CreatedAt_Field) value() interface{} {
	if !f._set || f._null {
		return nil
	}
	return f._value
}

func (BlockHeader_CreatedAt_Field) _Column() string { return "created_at" }

func toUTC(t time.Time) time.Time {
	return t.UTC()
}

func toDate(t time.Time) time.Time {
	// keep up the minute portion so that translations between timezones will
	// continue to reflect properly.
	return t.Truncate(time.Minute)
}

//
// runtime support for building sql statements
//

type __sqlbundle_SQL interface {
	Render() string

	private()
}

type __sqlbundle_Dialect interface {
	Rebind(sql string) string
}

type __sqlbundle_RenderOp int

const (
	__sqlbundle_NoFlatten __sqlbundle_RenderOp = iota
	__sqlbundle_NoTerminate
)

func __sqlbundle_Render(dialect __sqlbundle_Dialect, sql __sqlbundle_SQL, ops ...__sqlbundle_RenderOp) string {
	out := sql.Render()

	flatten := true
	terminate := true
	for _, op := range ops {
		switch op {
		case __sqlbundle_NoFlatten:
			flatten = false
		case __sqlbundle_NoTerminate:
			terminate = false
		}
	}

	if flatten {
		out = __sqlbundle_flattenSQL(out)
	}
	if terminate {
		out += ";"
	}

	return dialect.Rebind(out)
}

func __sqlbundle_flattenSQL(x string) string {
	// trim whitespace from beginning and end
	s, e := 0, len(x)-1
	for s < len(x) && (x[s] == ' ' || x[s] == '\t' || x[s] == '\n') {
		s++
	}
	for s <= e && (x[e] == ' ' || x[e] == '\t' || x[e] == '\n') {
		e--
	}
	if s > e {
		return ""
	}
	x = x[s : e+1]

	// check for whitespace that needs fixing
	wasSpace := false
	for i := 0; i < len(x); i++ {
		r := x[i]
		justSpace := r == ' '
		if (wasSpace && justSpace) || r == '\t' || r == '\n' {
			// whitespace detected, start writing a new string
			var result strings.Builder
			result.Grow(len(x))
			if wasSpace {
				result.WriteString(x[:i-1])
			} else {
				result.WriteString(x[:i])
			}
			for p := i; p < len(x); p++ {
				for p < len(x) && (x[p] == ' ' || x[p] == '\t' || x[p] == '\n') {
					p++
				}
				result.WriteByte(' ')

				start := p
				for p < len(x) && !(x[p] == ' ' || x[p] == '\t' || x[p] == '\n') {
					p++
				}
				result.WriteString(x[start:p])
			}

			return result.String()
		}
		wasSpace = justSpace
	}

	// no problematic whitespace found
	return x
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_postgres struct{}

func (p __sqlbundle_postgres) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_sqlite3 struct{}

func (s __sqlbundle_sqlite3) Rebind(sql string) string {
	return sql
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_cockroach struct{}

func (p __sqlbundle_cockroach) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_pgx struct{}

func (p __sqlbundle_pgx) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

// this type is specially named to match up with the name returned by the
// dialect impl in the sql package.
type __sqlbundle_pgxcockroach struct{}

func (p __sqlbundle_pgxcockroach) Rebind(sql string) string {
	type sqlParseState int
	const (
		sqlParseStart sqlParseState = iota
		sqlParseInStringLiteral
		sqlParseInQuotedIdentifier
		sqlParseInComment
	)

	out := make([]byte, 0, len(sql)+10)

	j := 1
	state := sqlParseStart
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch state {
		case sqlParseStart:
			switch ch {
			case '?':
				out = append(out, '$')
				out = append(out, strconv.Itoa(j)...)
				state = sqlParseStart
				j++
				continue
			case '-':
				if i+1 < len(sql) && sql[i+1] == '-' {
					state = sqlParseInComment
				}
			case '"':
				state = sqlParseInQuotedIdentifier
			case '\'':
				state = sqlParseInStringLiteral
			}
		case sqlParseInStringLiteral:
			if ch == '\'' {
				state = sqlParseStart
			}
		case sqlParseInQuotedIdentifier:
			if ch == '"' {
				state = sqlParseStart
			}
		case sqlParseInComment:
			if ch == '\n' {
				state = sqlParseStart
			}
		}
		out = append(out, ch)
	}

	return string(out)
}

type __sqlbundle_Literal string

func (__sqlbundle_Literal) private() {}

func (l __sqlbundle_Literal) Render() string { return string(l) }

type __sqlbundle_Literals struct {
	Join string
	SQLs []__sqlbundle_SQL
}

func (__sqlbundle_Literals) private() {}

func (l __sqlbundle_Literals) Render() string {
	var out bytes.Buffer

	first := true
	for _, sql := range l.SQLs {
		if sql == nil {
			continue
		}
		if !first {
			out.WriteString(l.Join)
		}
		first = false
		out.WriteString(sql.Render())
	}

	return out.String()
}

type __sqlbundle_Condition struct {
	// set at compile/embed time
	Name  string
	Left  string
	Equal bool
	Right string

	// set at runtime
	Null bool
}

func (*__sqlbundle_Condition) private() {}

func (c *__sqlbundle_Condition) Render() string {
	// TODO(jeff): maybe check if we can use placeholders instead of the
	// literal null: this would make the templates easier.

	switch {
	case c.Equal && c.Null:
		return c.Left + " is null"
	case c.Equal && !c.Null:
		return c.Left + " = " + c.Right
	case !c.Equal && c.Null:
		return c.Left + " is not null"
	case !c.Equal && !c.Null:
		return c.Left + " != " + c.Right
	default:
		panic("unhandled case")
	}
}

type __sqlbundle_Hole struct {
	// set at compiile/embed time
	Name string

	// set at runtime or possibly embed time
	SQL __sqlbundle_SQL
}

func (*__sqlbundle_Hole) private() {}

func (h *__sqlbundle_Hole) Render() string {
	if h.SQL == nil {
		return ""
	}
	return h.SQL.Render()
}

//
// end runtime support for building sql statements
//

func (obj *pgxImpl) Create_BlockHeader(ctx context.Context,
	block_header_hash BlockHeader_Hash_Field,
	block_header_number BlockHeader_Number_Field,
	block_header_timestamp BlockHeader_Timestamp_Field) (
	block_header *BlockHeader, err error) {
	defer mon.Task()(&ctx)(&err)
	__hash_val := block_header_hash.value()
	__number_val := block_header_number.value()
	__timestamp_val := block_header_timestamp.value()

	var __columns = &__sqlbundle_Hole{SQL: __sqlbundle_Literal("hash, number, timestamp")}
	var __placeholders = &__sqlbundle_Hole{SQL: __sqlbundle_Literal("?, ?, ?")}
	var __clause = &__sqlbundle_Hole{SQL: __sqlbundle_Literals{Join: "", SQLs: []__sqlbundle_SQL{__sqlbundle_Literal("("), __columns, __sqlbundle_Literal(") VALUES ("), __placeholders, __sqlbundle_Literal(")")}}}

	var __embed_stmt = __sqlbundle_Literals{Join: "", SQLs: []__sqlbundle_SQL{__sqlbundle_Literal("INSERT INTO block_headers "), __clause, __sqlbundle_Literal(" RETURNING block_headers.hash, block_headers.number, block_headers.timestamp, block_headers.created_at")}}

	var __values []interface{}
	__values = append(__values, __hash_val, __number_val, __timestamp_val)

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	block_header = &BlockHeader{}
	err = obj.queryRowContext(ctx, __stmt, __values...).Scan(&block_header.Hash, &block_header.Number, &block_header.Timestamp, &block_header.CreatedAt)
	if err != nil {
		return nil, obj.makeErr(err)
	}
	return block_header, nil

}

func (obj *pgxImpl) All_BlockHeader_OrderBy_Desc_Timestamp(ctx context.Context) (
	rows []*BlockHeader, err error) {
	defer mon.Task()(&ctx)(&err)

	var __embed_stmt = __sqlbundle_Literal("SELECT block_headers.hash, block_headers.number, block_headers.timestamp, block_headers.created_at FROM block_headers ORDER BY block_headers.timestamp DESC")

	var __values []interface{}

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	for {
		rows, err = func() (rows []*BlockHeader, err error) {
			__rows, err := obj.driver.QueryContext(ctx, __stmt, __values...)
			if err != nil {
				return nil, err
			}
			defer __rows.Close()

			for __rows.Next() {
				block_header := &BlockHeader{}
				err = __rows.Scan(&block_header.Hash, &block_header.Number, &block_header.Timestamp, &block_header.CreatedAt)
				if err != nil {
					return nil, err
				}
				rows = append(rows, block_header)
			}
			if err := __rows.Err(); err != nil {
				return nil, err
			}
			return rows, nil
		}()
		if err != nil {
			if obj.shouldRetry(err) {
				continue
			}
			return nil, obj.makeErr(err)
		}
		return rows, nil
	}

}

func (obj *pgxImpl) Get_BlockHeader_By_Hash(ctx context.Context,
	block_header_hash BlockHeader_Hash_Field) (
	block_header *BlockHeader, err error) {
	defer mon.Task()(&ctx)(&err)

	var __embed_stmt = __sqlbundle_Literal("SELECT block_headers.hash, block_headers.number, block_headers.timestamp, block_headers.created_at FROM block_headers WHERE block_headers.hash = ?")

	var __values []interface{}
	__values = append(__values, block_header_hash.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	block_header = &BlockHeader{}
	err = obj.queryRowContext(ctx, __stmt, __values...).Scan(&block_header.Hash, &block_header.Number, &block_header.Timestamp, &block_header.CreatedAt)
	if err != nil {
		return (*BlockHeader)(nil), obj.makeErr(err)
	}
	return block_header, nil

}

func (obj *pgxImpl) Get_BlockHeader_By_Number(ctx context.Context,
	block_header_number BlockHeader_Number_Field) (
	block_header *BlockHeader, err error) {
	defer mon.Task()(&ctx)(&err)

	var __embed_stmt = __sqlbundle_Literal("SELECT block_headers.hash, block_headers.number, block_headers.timestamp, block_headers.created_at FROM block_headers WHERE block_headers.number = ? LIMIT 2")

	var __values []interface{}
	__values = append(__values, block_header_number.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	for {
		block_header, err = func() (block_header *BlockHeader, err error) {
			__rows, err := obj.driver.QueryContext(ctx, __stmt, __values...)
			if err != nil {
				return nil, err
			}
			defer __rows.Close()

			if !__rows.Next() {
				if err := __rows.Err(); err != nil {
					return nil, err
				}
				return nil, sql.ErrNoRows
			}

			block_header = &BlockHeader{}
			err = __rows.Scan(&block_header.Hash, &block_header.Number, &block_header.Timestamp, &block_header.CreatedAt)
			if err != nil {
				return nil, err
			}

			if __rows.Next() {
				return nil, errTooManyRows
			}

			if err := __rows.Err(); err != nil {
				return nil, err
			}

			return block_header, nil
		}()
		if err != nil {
			if obj.shouldRetry(err) {
				continue
			}
			if err == errTooManyRows {
				return nil, tooManyRows("BlockHeader_By_Number")
			}
			return nil, obj.makeErr(err)
		}
		return block_header, nil
	}

}

func (obj *pgxImpl) First_BlockHeader_By_Timestamp_Greater(ctx context.Context,
	block_header_timestamp_greater BlockHeader_Timestamp_Field) (
	block_header *BlockHeader, err error) {
	defer mon.Task()(&ctx)(&err)

	var __embed_stmt = __sqlbundle_Literal("SELECT block_headers.hash, block_headers.number, block_headers.timestamp, block_headers.created_at FROM block_headers WHERE block_headers.timestamp > ? LIMIT 1 OFFSET 0")

	var __values []interface{}
	__values = append(__values, block_header_timestamp_greater.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	for {
		block_header, err = func() (block_header *BlockHeader, err error) {
			__rows, err := obj.driver.QueryContext(ctx, __stmt, __values...)
			if err != nil {
				return nil, err
			}
			defer __rows.Close()

			if !__rows.Next() {
				if err := __rows.Err(); err != nil {
					return nil, err
				}
				return nil, nil
			}

			block_header = &BlockHeader{}
			err = __rows.Scan(&block_header.Hash, &block_header.Number, &block_header.Timestamp, &block_header.CreatedAt)
			if err != nil {
				return nil, err
			}

			return block_header, nil
		}()
		if err != nil {
			if obj.shouldRetry(err) {
				continue
			}
			return nil, obj.makeErr(err)
		}
		return block_header, nil
	}

}

func (obj *pgxImpl) Delete_BlockHeader_By_Hash(ctx context.Context,
	block_header_hash BlockHeader_Hash_Field) (
	deleted bool, err error) {
	defer mon.Task()(&ctx)(&err)

	var __embed_stmt = __sqlbundle_Literal("DELETE FROM block_headers WHERE block_headers.hash = ?")

	var __values []interface{}
	__values = append(__values, block_header_hash.value())

	var __stmt = __sqlbundle_Render(obj.dialect, __embed_stmt)
	obj.logStmt(__stmt, __values...)

	__res, err := obj.driver.ExecContext(ctx, __stmt, __values...)
	if err != nil {
		return false, obj.makeErr(err)
	}

	__count, err := __res.RowsAffected()
	if err != nil {
		return false, obj.makeErr(err)
	}

	return __count > 0, nil

}

func (impl pgxImpl) isConstraintError(err error) (
	constraint string, ok bool) {
	if e, ok := err.(*pgconn.PgError); ok {
		if e.Code[:2] == "23" {
			return e.ConstraintName, true
		}
	}
	return "", false
}

func (obj *pgxImpl) deleteAll(ctx context.Context) (count int64, err error) {
	defer mon.Task()(&ctx)(&err)
	var __res sql.Result
	var __count int64
	__res, err = obj.driver.ExecContext(ctx, "DELETE FROM block_headers;")
	if err != nil {
		return 0, obj.makeErr(err)
	}

	__count, err = __res.RowsAffected()
	if err != nil {
		return 0, obj.makeErr(err)
	}
	count += __count

	return count, nil

}

type Rx struct {
	db *DB
	tx *Tx
}

func (rx *Rx) UnsafeTx(ctx context.Context) (unsafe_tx tagsql.Tx, err error) {
	tx, err := rx.getTx(ctx)
	if err != nil {
		return nil, err
	}
	return tx.Tx, nil
}

func (rx *Rx) getTx(ctx context.Context) (tx *Tx, err error) {
	if rx.tx == nil {
		if rx.tx, err = rx.db.Open(ctx); err != nil {
			return nil, err
		}
	}
	return rx.tx, nil
}

func (rx *Rx) Rebind(s string) string {
	return rx.db.Rebind(s)
}

func (rx *Rx) Commit() (err error) {
	if rx.tx != nil {
		err = rx.tx.Commit()
		rx.tx = nil
	}
	return err
}

func (rx *Rx) Rollback() (err error) {
	if rx.tx != nil {
		err = rx.tx.Rollback()
		rx.tx = nil
	}
	return err
}

func (rx *Rx) All_BlockHeader_OrderBy_Desc_Timestamp(ctx context.Context) (
	rows []*BlockHeader, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.All_BlockHeader_OrderBy_Desc_Timestamp(ctx)
}

func (rx *Rx) Create_BlockHeader(ctx context.Context,
	block_header_hash BlockHeader_Hash_Field,
	block_header_number BlockHeader_Number_Field,
	block_header_timestamp BlockHeader_Timestamp_Field) (
	block_header *BlockHeader, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.Create_BlockHeader(ctx, block_header_hash, block_header_number, block_header_timestamp)

}

func (rx *Rx) Delete_BlockHeader_By_Hash(ctx context.Context,
	block_header_hash BlockHeader_Hash_Field) (
	deleted bool, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.Delete_BlockHeader_By_Hash(ctx, block_header_hash)
}

func (rx *Rx) First_BlockHeader_By_Timestamp_Greater(ctx context.Context,
	block_header_timestamp_greater BlockHeader_Timestamp_Field) (
	block_header *BlockHeader, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.First_BlockHeader_By_Timestamp_Greater(ctx, block_header_timestamp_greater)
}

func (rx *Rx) Get_BlockHeader_By_Hash(ctx context.Context,
	block_header_hash BlockHeader_Hash_Field) (
	block_header *BlockHeader, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.Get_BlockHeader_By_Hash(ctx, block_header_hash)
}

func (rx *Rx) Get_BlockHeader_By_Number(ctx context.Context,
	block_header_number BlockHeader_Number_Field) (
	block_header *BlockHeader, err error) {
	var tx *Tx
	if tx, err = rx.getTx(ctx); err != nil {
		return
	}
	return tx.Get_BlockHeader_By_Number(ctx, block_header_number)
}

type Methods interface {
	All_BlockHeader_OrderBy_Desc_Timestamp(ctx context.Context) (
		rows []*BlockHeader, err error)

	Create_BlockHeader(ctx context.Context,
		block_header_hash BlockHeader_Hash_Field,
		block_header_number BlockHeader_Number_Field,
		block_header_timestamp BlockHeader_Timestamp_Field) (
		block_header *BlockHeader, err error)

	Delete_BlockHeader_By_Hash(ctx context.Context,
		block_header_hash BlockHeader_Hash_Field) (
		deleted bool, err error)

	First_BlockHeader_By_Timestamp_Greater(ctx context.Context,
		block_header_timestamp_greater BlockHeader_Timestamp_Field) (
		block_header *BlockHeader, err error)

	Get_BlockHeader_By_Hash(ctx context.Context,
		block_header_hash BlockHeader_Hash_Field) (
		block_header *BlockHeader, err error)

	Get_BlockHeader_By_Number(ctx context.Context,
		block_header_number BlockHeader_Number_Field) (
		block_header *BlockHeader, err error)
}

type TxMethods interface {
	Methods

	Rebind(s string) string
	Commit() error
	Rollback() error
}

type txMethods interface {
	TxMethods

	deleteAll(ctx context.Context) (int64, error)
	makeErr(err error) error
}

type DBMethods interface {
	Methods

	Schema() string
	Rebind(sql string) string
}

type dbMethods interface {
	DBMethods

	wrapTx(tx tagsql.Tx) txMethods
	makeErr(err error) error
}

func openpgx(source string) (*sql.DB, error) {
	return sql.Open("pgx", source)
}
