package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"

	"github.com/AdrianPrawda/movie-poll/api/handler"
	"github.com/AdrianPrawda/movie-poll/api/util"
	"github.com/huandu/go-sqlbuilder"
	"github.com/labstack/echo/v4"
	"github.com/qustavo/dotsql"
)

type ServerConfig struct {
	port    int
	poll_db string
	debug   bool
}

// TODO: Implement API request limits on all handlers (echo middleware?)
func main() {
	// prepare config
	cfg := new(ServerConfig)
	flag.IntVar(&cfg.port, "port", 35555, "api port")
	flag.StringVar(&cfg.poll_db, "polldb", "file::memory:?cache=shared", "sqlite connection string for the poll database")
	flag.BoolVar(&cfg.debug, "debug", false, "debug flag")
	flag.Parse()

	// prepare logger
	log := util.Logger{
		Debug: log.New(os.Stdout, "DEBUG: ", log.Ldate|log.Ltime),
		Info:  log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime),
		Warn:  log.New(os.Stdout, "WARN: ", log.Ldate|log.Ltime),
		Error: log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime|log.Llongfile),
		Fatal: log.New(os.Stdout, "FATAL: ", log.Ldate|log.Ltime|log.Llongfile),
	}
	if !cfg.debug {
		log.Debug.SetOutput(io.Discard)
	}

	log.Info.Println("Setting up databases")
	poll_db, err := setup(cfg, &log)
	if err != nil {
		log.Fatal.Fatal(err)
	}

	if err := serve(cfg, &log, poll_db); err != nil {
		log.Fatal.Fatal(err)
	}
	os.Exit(0)
}

func setup(cfg *ServerConfig, log *util.Logger) (*sql.DB, error) {
	// Set global default for sql query builder
	sqlbuilder.DefaultFlavor = sqlbuilder.SQLite

	// prepare DB
	r, err := regexp.Compile(":memory:")
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", cfg.poll_db)
	if err != nil {
		return nil, err
	}

	if r.MatchString(cfg.poll_db) {
		log.Info.Println("In-Memory database detected, adjusting connection settings")
		db.SetMaxIdleConns(2)
		db.SetConnMaxLifetime(0)
	}

	// check DB connection
	attempts := 0
	backoff := util.NewExpBackoffContext(context.Background(), 2*time.Second, 1.25)

	for {
		if attempts >= 5 {
			log.Fatal.Println("Could not establish connection to poll database")
			return nil, errors.New("could not establish connection to poll database")
		}
		ctx, cancel := backoff.Next()
		defer cancel()
		if err := db.PingContext(ctx); err == nil {
			break
		}
		attempts++
	}
	log.Info.Println("Successfully pinged poll DB")

	// ensure foreign key constraints are enabled (can't be done with a transaction)
	log.Debug.Println("Turning on foreign key constraints on DB")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "PRAGMA FOREIGN_KEYS = ON;", nil); err != nil {
		log.Error.Println("Couldn't set FK pragma")
		return nil, err
	}

	// prepare DB
	log.Debug.Println("Loading init sql file")
	// init file should be next to executable
	ex, err := os.Executable()
	if err != nil {
		return nil, err
	}
	fpath := path.Join(filepath.Dir(ex), "init.sql")

	ds, err := dotsql.LoadFromFile(fpath)
	if err != nil {
		log.Error.Println("Couldn't load init sql file")
		return nil, err
	}

	log.Debug.Println("Setting up init transcation")
	ctx, tx_cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer tx_cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Error.Print(err)
		return nil, err
	}
	defer tx.Rollback()

	// create poll databse
	log.Info.Println("Preparing poll table")
	query, err := ds.Raw("create-poll-table")
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(query, nil); err != nil {
		return nil, err
	}

	// create choice database
	log.Info.Println("Preparing choice table")
	query, err = ds.Raw("create-choice-table")
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(query, nil); err != nil {
		return nil, err
	}

	// create vote database
	log.Info.Println("Preparing vote table")
	query, err = ds.Raw("create-vote-table")
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(query, nil); err != nil {
		return nil, err
	}

	// create next poll database
	log.Info.Println("Preparing next poll table")
	query, err = ds.Raw("create-next-poll-table")
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(query, nil); err != nil {
		return nil, err
	}

	// try to commit
	log.Debug.Println("Commiting transaction")
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db, nil
}

func serve(cfg *ServerConfig, log *util.Logger, poll_db *sql.DB) error {
	e := echo.New()
	h := handler.NewHandler(poll_db, log)

	e.POST("/api/poll/v1/create", h.CreatePoll)
	e.GET("/api/poll/v1/data", h.GetPollData)
	e.DELETE("/api/poll/v1/delete", h.DeletePoll)
	e.POST("/api/poll/v1/vote", h.VotePoll)
	e.GET("/api/poll/v1/status", h.GetPollStatus)
	e.POST("/api/heartbeat", h.Heartbeat)

	log.Info.Printf("Start serving API on port %d\n", cfg.port)
	defer poll_db.Close()
	return e.Start(fmt.Sprintf(":%d", cfg.port))
}
