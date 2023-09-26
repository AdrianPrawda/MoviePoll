package handler

import (
	"context"
	"database/sql"

	"github.com/AdrianPrawda/movie-poll/api/messages"
	"github.com/AdrianPrawda/movie-poll/api/util"
	"github.com/huandu/go-sqlbuilder"
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

	tx, err := q._db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	auto := 0
	if auto_create {
		auto = 1
	}
	insert_poll := sqlbuilder.NewInsertBuilder().InsertInto("poll").
		Cols("id", "title", "type", "target_votes", "auto_create").
		Values(id, title, string(poll_type), int(target_votes), auto)
	if _, err := tx.Exec(insert_poll.Build()); err != nil {
		return err
	}

	if prev_poll != "" {
		insert_next := sqlbuilder.NewInsertBuilder().InsertInto("next_poll").
			Cols("poll_id", "next_poll").
			Values(prev_poll, id)
		if _, err := tx.Exec(insert_next.Build()); err != nil {
			return err
		}
	}

	insert_choices := sqlbuilder.NewInsertBuilder().InsertInto("choice").
		Cols("poll_id", "content")
	for _, text := range choices {
		insert_choices.Values(id, text)
	}
	if _, err := tx.Exec(insert_choices.Build()); err != nil {
		return err
	}

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
	)

	tx, err := q._db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()

	// check if voting has already concluded
	var cast_votes, target_votes uint
	var auto_create int
	var title string
	if err := tx.QueryRow(STMT_POLL_DATA, poll).
		Scan(&cast_votes, &target_votes, &auto_create, &title); err != nil {
		return false, false, err
	}
	if cast_votes >= target_votes {
		return false, false, nil
	}

	// check if user has already voted
	var user_votes int
	if err := tx.QueryRow(STMT_USER_VOTES, poll, user).Scan(&user_votes); err != nil {
		return false, false, err
	}
	if user_votes != 0 {
		return false, false, nil
	}

	// insert votes
	insert_votes := sqlbuilder.NewInsertBuilder().InsertInto("vote").
		Cols("poll_id", "choice_id", "user")
	for _, choice := range votes {
		insert_votes.Values(poll, choice, user)
	}
	if _, err := tx.Exec(insert_votes.Build()); err != nil {
		if err == sqlite3.ErrBusySnapshot {
			return true, false, nil
		}
		return false, false, err
	}

	// increase votes in poll table
	if _, err := tx.Exec(STMT_UPDATE_POLL, poll); err != nil {
		if err == sqlite3.ErrBusySnapshot {
			return true, false, nil
		}
		return false, false, err
	}

	// check if changes can be commited
	if err := tx.Commit(); err != nil {
		if err == sqlite3.ErrBusySnapshot {
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
	var auto_create int
	data := new(pollData)
	if err := q._db.QueryRowContext(ctx, STMT, id).
		Scan(&data.title, &poll_type, &data.cast_votes, &data.target_votes, &auto_create); err != nil {
		return pollData{}, err
	}

	data.poll_type = messages.PollType(poll_type)
	data.auto_create = false
	if auto_create != 0 {
		data.auto_create = true
	}

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
