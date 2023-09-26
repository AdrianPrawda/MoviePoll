package messages

// Messages and types for /api/poll/v1/create
type CreatePollReq struct {
	Title       string   `json:"title"`
	TargetVotes uint     `json:"votes"`
	Choices     []string `json:"choices"`
	Type        PollType `json:"type"`
	PrevPollID  string   `json:"previous_poll_id"`
	AutoCreate  bool     `json:"auto_create"`
}

type CreatePollResp struct {
	PollID string `json:"poll_id"`
}

type PollType string

const (
	MULTIPLE PollType = "multiple"
	SINGLE   PollType = "single"
)

// Messages and types for /api/poll/v1/vote

type VotePollReq struct {
	PollID string `json:"poll_id"`
	UserID string `json:"user_id"`
	Votes  []int  `json:"votes"` // mapped to choice_id
}

// Messages and types for /api/poll/v1/delete

type DeletePollReq struct {
	PollID string `json:"poll_id"`
}

// Messages and types for /api/poll/v1/data

type GetPollDataReq struct {
	PollID string `json:"poll_id"`
}

type GetPollDataResp struct {
	VotesRequired uint           `json:"votes_required"`
	VotesCast     uint           `json:"votes_cast"`
	Type          PollType       `json:"type"`
	Choices       map[int]string `json:"choices"` // id -> text
	Votes         map[int]uint   `json:"votes"`   // id -> number of votes
	NextPoll      string         `json:"next_poll"`
	LatestPoll    string         `json:"latest_poll"`
}

// Messages and types for /api/poll/v1/status

type GetPollStatusReq struct {
	PollID string `json:"poll_id"`
}

type GetPollStatusResp struct {
	VotesRequired uint   `json:"votes_required"`
	VotesCast     uint   `json:"votes_cast"`
	NextPoll      string `json:"next_poll"`
}
