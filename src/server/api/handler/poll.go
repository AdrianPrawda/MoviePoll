package handler

import (
	"database/sql"
	"net/http"

	"github.com/AdrianPrawda/movie-poll/api/messages"
	"github.com/AdrianPrawda/movie-poll/api/util"
	"github.com/labstack/echo/v4"
	"github.com/mattn/go-sqlite3"
)

func (h *Handler) CreatePoll(c echo.Context) error {
	log := h.log
	log.Debug.Println("Creating Poll")

	req := new(messages.CreatePollReq)
	if err := c.Bind(req); err != nil {
		log.Error.Print(err)
		return c.NoContent(http.StatusBadRequest)
	}

	// validate user input
	log.Debug.Println("Validating input")
	if len(req.Choices) < 2 {
		log.Warn.Println("To few choices in request to create poll")
		return c.String(http.StatusBadRequest, "at least two choices must be provided")
	}
	if req.Title == "" {
		log.Warn.Println("Title must be at least 1 character long")
		return c.String(http.StatusBadRequest, "title must be at least 1 character long")
	}
	if req.TargetVotes < 1 {
		log.Warn.Println("Poll must allow for at least 1 vote")
		return c.String(http.StatusBadRequest, "poll must allow for at least 1 vote")
	}

	ctx, cancel := defaultTimeout()
	defer cancel()

	// create new poll
	poll_id := util.GenerateID()
	log.Debug.Println("Inserting poll data")
	err := h.queries.insertPoll(
		ctx,
		poll_id,
		req.Title,
		req.Type,
		req.TargetVotes,
		req.Choices,
		req.AutoCreate,
		req.PrevPollID)

	if err != nil {
		if h.isSQLiteErrNo(err, sqlite3.ErrConstraint) {
			log.Warn.Printf("Invalid previous poll id %s\n", req.PrevPollID)
			return c.String(http.StatusNotFound, "previous poll not found")
		}
		return h.handleError(c, err, true)
	}

	log.Debug.Println("Done inserting poll data")
	resp := messages.CreatePollResp{PollID: poll_id}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) VotePoll(c echo.Context) error {
	log := h.log
	req := new(messages.VotePollReq)
	if err := c.Bind(req); err != nil {
		log.Error.Print(err)
		return c.NoContent(http.StatusBadRequest)
	}
	log.Debug.Printf("user %s voting on poll %s\n", req.UserID, req.PollID)

	// validate input
	if len(req.Votes) == 0 {
		log.Warn.Println("no votes specified in request")
		return c.String(http.StatusBadRequest, "no votes specified")
	}
	if util.HasDuplicates[int](req.Votes) {
		log.Warn.Println("request contains duplicate votes")
		return c.String(http.StatusBadRequest, "duplicate votes are not allowed")
	}

	// timeout for the entire request
	ctx, cancel := defaultTimeout()
	defer cancel()

	// get votes and poll type
	log.Debug.Println("fetching poll data")
	data, err := h.queries.getPollData(ctx, req.PollID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn.Printf("can't vote, poll %s not found\n", req.PollID)
			return c.String(http.StatusNotFound, "poll not found")
		}
		return h.handleError(c, err, false)
	}

	// validate voting limits
	log.Debug.Println("validating voting limits")
	if data.cast_votes >= data.target_votes {
		return c.String(http.StatusBadRequest, "vote limit reached")
	}
	if data.poll_type == messages.SINGLE && len(req.Votes) != 1 {
		return c.String(http.StatusBadRequest, "to many / to few votes for selected poll")
	}

	// try to insert votes
	attempts := 0
	success := false
	for attempts > 5 || !success {
		retry, ok, err := h.queries.tryInsertVotes(ctx, req.PollID, req.UserID, req.Votes)
		if err != nil {
			if err == sql.ErrNoRows {
				h.log.Warn.Printf("Can't insert votem poll id %s or user id %s not found\n", req.PollID, req.UserID)
				return c.String(http.StatusNotFound, "poll id or user id not found")
			}
			return h.handleError(c, err, false)
		}
		if !retry && !ok {
			h.log.Warn.Printf("invalid voting request from user %s\n", req.UserID)
			return c.String(http.StatusBadRequest, "voting constraints not met")
		}
		success = ok
		attempts++
	}

	// to many attempts
	if attempts > 5 {
		h.log.Error.Println("Could not insert votes: snapshot busy (database overload or stuck queries?)")
		return c.String(http.StatusInternalServerError, "try again later")
	}

	if data.auto_create {
		// update data and check if a new poll should be created
		data, err = h.queries.getPollData(ctx, req.PollID)
		if err != nil {
			if err == sql.ErrNoRows {
				log.Warn.Printf("can't vote, poll %s not found\n", req.PollID)
				return c.String(http.StatusNotFound, "poll not found")
			}
			return h.handleError(c, err, false)
		}

		// creating a new poll should extend timeout limits
		pctx, pcancel := defaultTimeout()
		defer pcancel()

		// create new poll if voting target was met
		if data.cast_votes >= data.target_votes {
			old_choices, err := h.queries.getPollChoices(pctx, req.PollID)
			if err != nil {
				return h.handleError(c, err, true)
			}
			new_choices := make([]string, 0, len(old_choices))
			for _, choice := range old_choices {
				new_choices = append(new_choices, choice)
			}

			uuid := util.GenerateID()
			err = h.queries.insertPoll(
				pctx,
				uuid,
				data.title,
				messages.SINGLE,
				data.target_votes,
				new_choices,
				data.auto_create,
				req.PollID)
			if err != nil {
				return h.handleError(c, err, true)
			}
		}
	}

	return c.NoContent(http.StatusOK)
}

func (h *Handler) DeletePoll(c echo.Context) error {
	log := h.log
	req := new(messages.DeletePollReq)
	if err := c.Bind(req); err != nil {
		log.Error.Print(err)
		return c.NoContent(http.StatusBadRequest)
	}
	log.Debug.Printf("Deleting Poll %s\n", req.PollID)

	ctx, cancel := defaultTimeout()
	defer cancel()

	ok, err := h.queries.deletePoll(ctx, req.PollID)
	if err != nil {
		return h.handleError(c, err, true)
	}
	if !ok {
		log.Warn.Printf("Can't delete poll %s, poll not found\n", req.PollID)
		return c.String(http.StatusNotFound, "poll not found")
	}

	return c.NoContent(http.StatusOK)
}

func (h *Handler) GetPollData(c echo.Context) error {
	log := h.log
	req := new(messages.GetPollDataReq)
	if err := c.Bind(req); err != nil {
		log.Error.Print(err)
		return c.NoContent(http.StatusBadRequest)
	}
	log.Debug.Printf("Getting poll data for %s\n", req.PollID)

	ctx, cancel := defaultTimeout()
	defer cancel()

	// get poll data
	data, err := h.queries.getPollData(ctx, req.PollID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn.Printf("Can't get poll data, poll %s not found\n", req.PollID)
			return c.String(http.StatusNotFound, "poll not found")
		}
		return h.handleError(c, err, false)
	}

	// find next poll
	next_poll, ok, err := h.queries.getNextPoll(ctx, req.PollID)
	if err != nil {
		return h.handleError(c, err, true)
	}

	// find latest poll
	var latest_poll string
	if ok {
		latest_poll, _, err = h.queries.getLatestPoll(ctx, req.PollID)
		if err != nil {
			return h.handleError(c, err, true)
		}
	}

	// get poll choices
	choices, err := h.queries.getPollChoices(ctx, req.PollID)
	if err != nil {
		if err == sql.ErrNoRows {
			// should never happen at this point
			log.Error.Print(err)
			return c.NoContent(http.StatusInternalServerError)
		}
		return h.handleError(c, err, false)
	}

	// get poll votes
	votes, err := h.queries.getPollVotes(ctx, req.PollID)
	if err != nil {
		if err == sql.ErrNoRows {
			// should never happen at this point
			log.Error.Print(err)
			return c.NoContent(http.StatusInternalServerError)
		}
		return h.handleError(c, err, false)
	}

	// finish
	resp := messages.GetPollDataResp{
		VotesRequired: data.target_votes,
		VotesCast:     data.cast_votes,
		Type:          data.poll_type,
		Choices:       choices,
		Votes:         votes,
		NextPoll:      next_poll,
		LatestPoll:    latest_poll,
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetPollStatus(c echo.Context) error {
	log := h.log
	req := new(messages.GetPollStatusReq)
	if err := c.Bind(req); err != nil {
		log.Warn.Print(err)
		return c.NoContent(http.StatusBadRequest)
	}
	log.Debug.Printf("Getting poll status for %s\n", req.PollID)

	ctx, cancel := defaultTimeout()
	defer cancel()

	data, err := h.queries.getPollData(ctx, req.PollID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn.Printf("Can't get poll status, poll %s not found\n", req.PollID)
			return c.String(http.StatusNotFound, "poll not found")
		}
		return h.handleError(c, err, false)
	}

	next_poll, _, err := h.queries.getNextPoll(ctx, req.PollID)
	if err != nil && err != sql.ErrNoRows {
		return h.handleError(c, err, false)
	}

	resp := messages.GetPollStatusResp{
		VotesRequired: data.target_votes,
		VotesCast:     data.cast_votes,
		NextPoll:      next_poll,
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) Heartbeat(c echo.Context) error {
	h.log.Debug.Println("Heartbeat")
	ctx, cancel := defaultTimeout()
	defer cancel()
	if err := h.db.PingContext(ctx); err != nil {
		if err == sql.ErrConnDone || err == sql.ErrTxDone {
			h.log.Warn.Println("DB heartbeat failed")
			return c.NoContent(http.StatusRequestTimeout)
		}
		h.log.Error.Print(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.NoContent(http.StatusOK)
}
