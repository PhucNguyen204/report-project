package platform

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
)

type Error struct {
	Status  int
	Code    int
	Message string
}

func (e *Error) Error() string                   { return e.Message }
func Err(status, code int, message string) error { return &Error{status, code, message} }
func Invalid(message string) error               { return Err(400, 2002, message) }
func NotFound() error                            { return Err(404, 4001, "Không tìm thấy dữ liệu") }
func Forbidden() error                           { return Err(403, 3007, "Không có quyền thực hiện") }
func Conflict() error                            { return Err(409, 4002, "Dữ liệu đã thay đổi") }
func Rule(message string) error                  { return Err(422, 4004, message) }
func Write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	code := 1000
	if status == 202 {
		code = 1001
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "data": data})
}
func Fail(w http.ResponseWriter, err error) {
	e := &Error{500, 5000, "Lỗi hệ thống"}
	var typed *Error
	var pe *pgconn.PgError
	if errors.As(err, &typed) {
		e = typed
	} else if errors.Is(err, pgx.ErrNoRows) {
		e = &Error{404, 4001, "Không tìm thấy dữ liệu"}
	} else if errors.As(err, &pe) {
		switch pe.Code {
		case "23505":
			e = &Error{409, 4003, "Dữ liệu đã tồn tại"}
		case "23503", "23514", "22P02":
			e = &Error{422, 4004, "Dữ liệu không hợp lệ"}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	body := map[string]any{"code": e.Code, "message": e.Message}
	if id := w.Header().Get("X-Trace-ID"); id != "" {
		body["traceId"] = id
	}
	_ = json.NewEncoder(w).Encode(body)
}
