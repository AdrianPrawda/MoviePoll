--name: pragma_fk
PRAGMA FOREIGN_KEYS = ON;

-- name: create-poll-table
CREATE TABLE IF NOT EXISTS poll(
    id TEXT NOT NULL PRIMARY KEY,
    title TEXT NOT NULL,
    poll_type TEXT NOT NULL DEFAULT "single", --either single or multiple
    cast_votes INT NOT NULL CHECK(cast_votes >= 0) DEFAULT 0,
    target_votes INT NOT NULL CHECK(target_votes > 0), --number of votes needed for poll to conclude
    auto_create BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT current_timestamp
);

--name: create-choice-table
CREATE TABLE IF NOT EXISTS choice(
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    poll_id INT NOT NULL,
    content TEXT NOT NULL, --textural representation of that choice
    FOREIGN KEY(poll_id) REFERENCES poll(id) ON DELETE CASCADE
);

--name: create-vote-table
CREATE TABLE IF NOT EXISTS vote(
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    poll_id INT NOT NULL,
    choice_id INT NOT NULL,
    user TEXT NOT NULL, --user id
    created_at DATETIME NOT NULL DEFAULT current_timestamp,
    FOREIGN KEY(poll_id) REFERENCES poll(id) ON DELETE CASCADE,
    FOREIGN KEY(choice_id) REFERENCES choice(id) ON DELETE CASCADE
);

--name: create-next-poll-table
CREATE TABLE IF NOT EXISTS next_poll(
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    poll_id TEXT NOT NULL UNIQUE,
    next_poll INT NOT NULL UNIQUE,
    FOREIGN KEY(poll_id) REFERENCES poll(id) ON DELETE CASCADE,
    FOREIGN KEY(next_poll) REFERENCES poll(id) ON DELETE CASCADE
)