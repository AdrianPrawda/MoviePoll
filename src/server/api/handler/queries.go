package handler

import (
	"context"
	"database/sql"

	"github.com/AdrianPrawda/movie-poll/api/messages"
	"github.com/AdrianPrawda/movie-poll/api/util"
	"github.com/mattn/go-sqlite3"
)

type queryHandler struct {
	_db  *sql.DB
	_log *util.Logger
}

// Inserts a new poll into the database, including all data dependencies.
func (q *queryHandler) insertPoll(
	ctx context.Context,
	id string,
	title string,
	poll_type messages.PollType,
	target_votes uint,
	choices []string,
	auto_create bool,
	prev_poll string) error {

	const (
		STMT_INSERT_POLL   = "INSERT INTO poll (id, title, poll_type, target_votes, auto_create) VALUES (?,?,?,?,?)"
		STMT_INSERT_NEXT   = "INSERT INTO next_poll (poll_id, next_poll) VALUES (?,?)"
		STMT_INSERT_CHOICE = "INSERT INTO choice (poll_id, content) VALUES (?,?)"
	)

	debug := q._log.Debug
	debug.Println("Inserting poll")

	tx, err := q._db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// insert into poll
	debug.Println("Inserting into poll table")
	if _, err := tx.Exec(STMT_INSERT_POLL, id, title, poll_type, target_votes, auto_create); err != nil {
		return err
	}

	// insert into next_poll
	if prev_poll != "" {
		debug.Println("Inserting into next poll table")
		if _, err := tx.Exec(STMT_INSERT_NEXT, prev_poll, id); err != nil {
			return err
		}
	}

	// insert into choice
	debug.Println("Insert into choice table")
	stmt_insert_choice, err := tx.Prepare(STMT_INSERT_CHOICE)
	if err != nil {
		return err
	}
	for _, content := range choices {
		if _, err := stmt_insert_choice.Exec(id, content); err != nil {
			return err
		}
	}
	if err := stmt_insert_choice.Close(); err != nil {
		return err
	}

	debug.Println("Commiting")
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// Tries to insert votes into the voting table if constraints are met.
// Will not check if number of votes are correct.
// Returns if query can be retried and if insertion has been sucessfull
// Caller should check for sql.ErrNoRows in err.
func (q *queryHandler) tryInsertVotes(
	ctx context.Context,
	poll string,
	user string,
	votes []int) (bool, bool, error) {

	const (
		STMT_POLL_DATA   = "SELECT cast_votes, target_votes, auto_create, title FROM poll WHERE id=?"
		STMT_USER_VOTES  = "SELECT COUNT(*) FROM vote WHERE poll_id=? AND user=?"
		STMT_UPDATE_POLL = "UPDATE poll SET cast_votes = cast_votes + 1 WHERE id=?"
		STMT_INSERT_VOTE = "INSERT INTO vote (poll_id, choice_id, user) VALUES (?,?,?)"
	)

	debug := q._log.Debug

	tx, err := q._db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()

	// check if voting has already concluded
	debug.Println("Fetching poll data")
	var cast_votes, target_votes uint
	var auto_create bool
	var title string
	if err := tx.QueryRow(STMT_POLL_DATA, poll).
		Scan(&cast_votes, &target_votes, &auto_create, &title); err != nil {
		return false, false, err
	}
	if cast_votes >= target_votes {
		debug.Println("Target votes exceeded")
		return false, false, nil
	}

	// check if user has already voted
	debug.Println("Fetching number of user votes")
	var user_votes int
	if err := tx.QueryRow(STMT_USER_VOTES, poll, user).Scan(&user_votes); err != nil {
		return false, false, err
	}
	if user_votes != 0 {
		debug.Println("User already voted")
		return false, false, nil
	}

	// insert votes
	debug.Println("Inserting votes")
	stmt_insert_vote, err := tx.Prepare(STMT_INSERT_VOTE)
	if err != nil {
		return false, false, err
	}
	for _, choice := range votes {
		if _, err := tx.Stmt(stmt_insert_vote).Exec(poll, choice, user); err != nil {
			if err == sqlite3.ErrBusySnapshot {
				debug.Println("Snapshot busy")
				return true, false, nil
			}
			return false, false, err
		}
	}
	err = stmt_insert_vote.Close()
	if err != nil {
		return false, false, err
	}

	// increase votes in poll table
	debug.Println("Increasing poll votes")
	if _, err := tx.Exec(STMT_UPDATE_POLL, poll); err != nil {
		if err == sqlite3.ErrBusySnapshot {
			debug.Println("Snapshot busy")
			return true, false, nil
		}
		return false, false, err
	}

	// check if changes can be commited
	debug.Println("Commiting changes")
	if err := tx.Commit(); err != nil {
		if err == sqlite3.ErrBusySnapshot {
			debug.Println("Snapshot busy")
			return true, false, nil
		}
		return false, false, err
	}

	return false, true, nil
}

// Deletes the specified poll. Returns true if sucessfull.
func (q *queryHandler) deletePoll(
	ctx context.Context,
	id string) (bool, error) {

	res, err := q._db.ExecContext(ctx, "DELETE FROM poll WHERE id=?", id)
	if err != nil {
		return false, err
	}

	changes, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if changes == 0 {
		return false, nil
	}
	return true, nil
}

// Returns all available choices for a specified poll. Maps choice ids to textural representation.
func (q *queryHandler) getPollChoices(
	ctx context.Context,
	id string) (map[int]string, error) {

	var cid int
	var content string

	rows, err := q._db.QueryContext(ctx, "SELECT id, content FROM choice WHERE poll_id=?", id)
	if err != nil {
		return nil, err
	}

	choices := make(map[int]string, 2)
	for rows.Next() {
		if err := rows.Scan(&cid, &content); err != nil {
			return nil, err
		}
		choices[cid] = content
	}

	return choices, nil
}

// Returns all votes cast for a specified poll. Maps choice ids to number of votes.
func (q *queryHandler) getPollVotes(
	ctx context.Context,
	id string) (map[int]uint, error) {

	choices, err := q.getPollChoices(ctx, id)
	if err != nil {
		return nil, err
	}

	const STMT = "SELECT COUNT(*) FROM vote WHERE poll_id=? AND choice_id=?"
	var votes uint
	total_votes := make(map[int]uint, 0)
	for cid := range choices {
		if err := q._db.QueryRowContext(ctx, STMT, id, cid).Scan(&votes); err != nil {
			return nil, err
		}
		total_votes[cid] = votes
	}

	return total_votes, nil
}

type pollData struct {
	title        string
	poll_type    messages.PollType
	cast_votes   uint
	target_votes uint
	auto_create  bool
}

// Returns most row values from the poll table.
func (q *queryHandler) getPollData(
	ctx context.Context,
	id string) (pollData, error) {

	const STMT = "SELECT title, poll_type, cast_votes, target_votes, auto_create FROM poll WHERE id=?"
	var poll_type string
	var auto_create bool
	data := new(pollData)
	if err := q._db.QueryRowContext(ctx, STMT, id).
		Scan(&data.title, &poll_type, &data.cast_votes, &data.target_votes, &auto_create); err != nil {
		return pollData{}, err
	}

	data.poll_type = messages.PollType(poll_type)
	return *data, nil
}

// Returns next poll if it exists. Returns an empty string and false if no next poll exists
func (q *queryHandler) getNextPoll(
	ctx context.Context,
	id string) (string, bool, error) {

	const STMT = "SELECT next_poll FROM next_poll WHERE poll_id=?"
	var next_poll string
	if err := q._db.QueryRowContext(ctx, STMT, id).Scan(&next_poll); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return next_poll, true, nil
}

func (q *queryHandler) getLatestPoll(
	ctx context.Context,
	id string) (string, bool, error) {

	next_poll, exists, err := q.getNextPoll(ctx, id)
	if err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, nil
	}

	latest_poll := next_poll
	for {
		next, exists, err := q.getNextPoll(ctx, latest_poll)
		if err != nil {
			return "", false, err
		}
		if !exists {
			break
		}
		latest_poll = next
	}

	return latest_poll, true, nil
}
