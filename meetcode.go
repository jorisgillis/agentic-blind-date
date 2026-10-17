package main

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// meetCodeAlphabet is the alphabet meet codes are drawn from: unambiguous —
// no 0/O or 1/I — so a code read aloud or typed across a noisy room survives.
const meetCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// meetCodeLength is the length of a meet code.
const meetCodeLength = 6

// deriveMeetCode deterministically derives a meet code from a Participant id
// and a salt: the same id and salt always derive the same code, and a
// collision (astronomically unlikely) re-derives with the next salt.
func deriveMeetCode(id string, salt int) string {
	sum := sha256.Sum256([]byte(id + "#" + strconv.Itoa(salt)))
	code := make([]byte, meetCodeLength)
	for i := range code {
		code[i] = meetCodeAlphabet[int(sum[i])%len(meetCodeAlphabet)]
	}
	return string(code)
}

// normalizeMeetCode canonicalizes a typed meet code: trimmed and uppercased.
func normalizeMeetCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// meetCodeStore is the part of *sql.DB and *sql.Tx the meet-code assignment
// needs, so it runs inside CreateParticipant's transaction and standalone.
type meetCodeStore interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// ensureMeetCode returns the Participant's meet code, deriving and storing one
// when they don't have one yet. The code is derived deterministically from
// the id; a collision re-derives deterministically with a salt.
func ensureMeetCode(s meetCodeStore, id string) (string, error) {
	for salt := 0; ; salt++ {
		code, retry, err := meetCodeStep(s, id, salt)
		if err != nil {
			return "", err
		}
		if !retry {
			return code, nil
		}
	}
}

// meetCodeStep is one derivation attempt: the Participant's stored code when
// they have one, otherwise a freshly derived and stored one. retry reports
// that the attempt collided (or lost a race) and the next salt should be
// tried.
func meetCodeStep(s meetCodeStore, id string, salt int) (code string, retry bool, err error) {
	stored, err := readMeetCode(s, id)
	if err != nil {
		return "", false, err
	}
	if stored != "" {
		return stored, false, nil
	}
	assigned, err := assignMeetCode(s, id, salt)
	if err != nil {
		return "", false, err
	}
	if assigned != "" {
		return assigned, false, nil
	}
	return "", true, nil
}

// readMeetCode reads the Participant's stored meet code; "" when they have
// none.
func readMeetCode(s meetCodeStore, id string) (string, error) {
	var code string
	if err := s.QueryRow(`SELECT COALESCE(meet_code, '') FROM participants WHERE id = ?`, id).Scan(&code); err != nil {
		return "", err
	}
	return code, nil
}

// assignMeetCode derives and stores the Participant's meet code when the row
// has none yet. A UNIQUE collision returns "" without error: the caller
// re-derives with the next salt.
func assignMeetCode(s meetCodeStore, id string, salt int) (string, error) {
	candidate := deriveMeetCode(id, salt)
	res, err := s.Exec(`UPDATE participants SET meet_code = ? WHERE id = ? AND (meet_code IS NULL OR meet_code = '')`, candidate, id)
	if isUniqueViolation(err) {
		return "", nil // collision: the caller re-derives with the next salt
	}
	if err != nil {
		return "", err
	}
	assigned := "" // a lost race: the row got a code meanwhile
	if rowsAffected(res) == 1 {
		assigned = candidate
	}
	return assigned, nil
}

// isUniqueViolation reports whether err is SQLite's UNIQUE constraint
// violation — the backstop behind ensureMeetCode's collision re-derivation.
func isUniqueViolation(err error) bool {
	var serr sqlite3.Error
	return errors.As(err, &serr) && serr.ExtendedCode == sqlite3.ErrConstraintUnique
}

// backfillMeetCodes assigns meet codes to the Participants who registered
// before meet codes existed. It runs once at startup (NewDB).
func backfillMeetCodes(db *DB) error {
	ids, err := participantsWithoutMeetCode(db)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := ensureMeetCode(db.db, id); err != nil {
			return err
		}
	}
	return nil
}

// participantsWithoutMeetCode lists the ids of the Participants with no meet
// code.
func participantsWithoutMeetCode(db *DB) ([]string, error) {
	rows, err := db.db.Query(`SELECT id FROM participants WHERE meet_code IS NULL OR meet_code = ''`)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id) // a TEXT column into a string: cannot fail
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
