package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/AdrianPrawda/movie-poll/api/util"
	"github.com/labstack/echo/v4"
	"github.com/mattn/go-sqlite3"
)

type Handler struct {
	db      *sql.DB
	log     *util.Logger
	queries *queryHandler
}

func NewHandler(db *sql.DB, log *util.Logger) Handler {
	return Handler{db, log, &queryHandler{db, log}}
}

func defaultTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (h *Handler) handleError(c echo.Context, err error, noRowsErr bool) error {
	lwarn := h.log.Warn
	lerr := h.log.Error

	if err == nil {
		return c.NoContent(http.StatusOK)
	}
	// sql.ErrNoRows is not considered an error
	if err == sql.ErrNoRows {
		if noRowsErr {
			lerr.Println("query returned no rows")
			return c.NoContent(http.StatusInternalServerError)
		}
		lwarn.Println("sql.ErrNoRows should be handled externally")
		return c.NoContent(http.StatusOK)
	}

	if err == sql.ErrConnDone || err == sql.ErrTxDone {
		lwarn.Print("Request timed out")
		return c.NoContent(http.StatusRequestTimeout)
	}

	// handle sqlite specific errors
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		switch e := sqliteErr.Code; e {
		case sqlite3.ErrAbort:
			lwarn.Printf("DB operation aborted (most likely due transaction errors): %v", sqliteErr)
			return c.NoContent(http.StatusInternalServerError)
		case sqlite3.ErrBusy:
			lwarn.Println("Database is busy")
			return c.String(http.StatusInternalServerError, "Try again later")
		case sqlite3.ErrAuth:
			lerr.Printf("Unauthorized database access: %v", sqliteErr)
			return c.NoContent(http.StatusInternalServerError)
		case sqlite3.ErrReadonly:
			lerr.Printf("Can't modify data on a read-only connection: %v", sqliteErr)
			return c.NoContent(http.StatusInternalServerError)
		case sqlite3.ErrConstraint:
			lwarn.Printf("Can't modify data, constraint failed: %s\n", sqliteErr.ExtendedCode.Error())
			return c.NoContent(http.StatusBadRequest)
		default:
			lerr.Print(sqliteErr)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	lerr.Print(err)
	return c.NoContent(http.StatusInternalServerError)
}

// Checks if database was busy. Not yet used.
func (h *Handler) isDBBusy(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrBusy
	}
	return false
}

func (h *Handler) isSQLiteErrNo(err error, errno sqlite3.ErrNo) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == errno
	}
	return false
}
