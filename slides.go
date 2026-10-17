package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxSlideDeckBytes is the largest PDF the Host can upload. A variable only so
// tests can shrink it.
var maxSlideDeckBytes int64 = 100 << 20

// pdfMagic is how every PDF starts.
const pdfMagic = "%PDF-"

// SlideDeck is a meet-up speaker's slides: one PDF the Host uploaded, with a
// title and the speaker's name as plain text. The speaker is not a
// Participant, and a Slide Deck has nothing to do with Show & Tell.
type SlideDeck struct {
	ID        int64
	Title     string
	Speaker   string
	Filename  string // the name the PDF was uploaded under, shown on download
	Size      int64
	CreatedAt time.Time
	stored    string // the generated name of the file on disk
}

// ErrSlideDeckNotFound is returned for an unknown Slide Deck id.
var ErrSlideDeckNotFound = errors.New("slide deck not found")

// InvalidSlideDeckError reports an upload that cannot become a Slide Deck.
// Its message is safe to show to the Host.
type InvalidSlideDeckError struct{ msg string }

func (e *InvalidSlideDeckError) Error() string { return e.msg }

// SlideDecks owns the slide_decks table and the PDFs on the data volume. Each
// PDF is stored under a generated name, never the uploaded filename.
type SlideDecks struct {
	db  *DB
	dir string
}

// NewSlideDecks creates the Slide Decks module; the PDFs live in dir.
func NewSlideDecks(db *DB, dir string) *SlideDecks {
	return &SlideDecks{db: db, dir: dir}
}

// Add stores an uploaded PDF as a new Slide Deck. The title and the speaker's
// name are required, the filename must end in .pdf, and the content must start
// like a PDF and fit the size cap. A failure leaves neither a row nor a file.
func (s *SlideDecks) Add(title, speaker, filename string, content io.Reader) (*SlideDeck, error) {
	title, speaker, filename, err := normalizeDeckInput(title, speaker, filename)
	if err != nil {
		return nil, err
	}
	if err := requirePDF(content); err != nil {
		return nil, err
	}
	stored, size, err := s.writeDeckFile(content)
	if err != nil {
		return nil, err
	}
	deck := &SlideDeck{Title: title, Speaker: speaker, Filename: filename, Size: size, stored: stored}
	if err := s.insertDeck(deck, filepath.Join(s.dir, stored)); err != nil {
		return nil, err
	}
	deck.CreatedAt = time.Now()
	return deck, nil
}

// normalizeDeckInput trims the upload's fields and checks the ones that are
// required: the title, the speaker's name, and a filename ending in .pdf.
func normalizeDeckInput(title, speaker, filename string) (string, string, string, error) {
	title = strings.TrimSpace(title)
	speaker = strings.TrimSpace(speaker)
	filename = filepath.Base(strings.TrimSpace(filename))
	switch {
	case title == "":
		return "", "", "", &InvalidSlideDeckError{"Give the slides a title."}
	case speaker == "":
		return "", "", "", &InvalidSlideDeckError{"Give the speaker's name."}
	case !strings.EqualFold(filepath.Ext(filename), ".pdf"):
		return "", "", "", &InvalidSlideDeckError{"The slides must be a PDF file."}
	}
	return title, speaker, filename, nil
}

// requirePDF checks that the upload's content starts like a PDF, consuming
// the magic bytes.
func requirePDF(content io.Reader) error {
	magic := make([]byte, len(pdfMagic))
	if _, err := io.ReadFull(content, magic); err != nil || string(magic) != pdfMagic {
		return &InvalidSlideDeckError{"That file is not a PDF."}
	}
	return nil
}

// writeDeckFile writes the PDF — magic bytes included — under a generated
// name, and returns the name and the size. A failed write leaves no file.
func (s *SlideDecks) writeDeckFile(content io.Reader) (stored string, size int64, err error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", 0, err
	}
	stored = uuid.NewString() + ".pdf"
	path := filepath.Join(s.dir, stored)
	size, err = writeCapped(path, io.MultiReader(strings.NewReader(pdfMagic), content))
	if err != nil {
		_ = os.Remove(path) // best effort: Sweep removes any orphan
		return "", 0, err
	}
	return stored, size, nil
}

// insertDeck records the written file as a new Slide Deck; on failure the
// written file goes too — best effort: Sweep removes any orphan.
func (s *SlideDecks) insertDeck(deck *SlideDeck, path string) error {
	err := s.db.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO slide_decks (title, speaker, filename, stored_name, size) VALUES (?, ?, ?, ?, ?)`,
			deck.Title, deck.Speaker, deck.Filename, deck.stored, deck.Size)
		if err != nil {
			return err
		}
		deck.ID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// writeCapped writes r to a new file at path, refusing more than the size cap.
func writeCapped(path string, r io.Reader) (int64, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxSlideDeckBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if n > maxSlideDeckBytes {
		return 0, &InvalidSlideDeckError{fmt.Sprintf("That file is too large (the limit is %d MB).", maxSlideDeckBytes>>20)}
	}
	return n, nil
}

const selectSlideDeck = `SELECT id, title, speaker, filename, stored_name, size, created_at FROM slide_decks`

func scanSlideDeck(row interface{ Scan(...any) error }) (*SlideDeck, error) {
	d := &SlideDeck{}
	err := row.Scan(&d.ID, &d.Title, &d.Speaker, &d.Filename, &d.stored, &d.Size, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// List returns every Slide Deck in upload order.
func (s *SlideDecks) List() ([]SlideDeck, error) {
	rows, err := s.db.db.Query(selectSlideDeck + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var decks []SlideDeck
	for rows.Next() {
		d, err := scanSlideDeck(rows)
		if err != nil {
			return nil, err
		}
		decks = append(decks, *d)
	}
	return decks, rows.Err()
}

// Get returns one Slide Deck, or ErrSlideDeckNotFound.
func (s *SlideDecks) Get(id int64) (*SlideDeck, error) {
	d, err := scanSlideDeck(s.db.db.QueryRow(selectSlideDeck+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSlideDeckNotFound
	}
	return d, err
}

// Open returns the Slide Deck and its PDF, which the caller closes. A deck
// whose file has gone missing is not found.
func (s *SlideDecks) Open(id int64) (*SlideDeck, *os.File, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, nil, err
	}
	// The stored name is always the generated uuid name Add wrote (never
	// the upload's filename), so nothing user-controlled reaches the join.
	f, err := os.Open(filepath.Join(s.dir, d.stored)) //nolint:gosec // G703: stored is the generated uuid name, not user input
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrSlideDeckNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return d, f, nil
}

// Remove deletes a Slide Deck and its file. The row goes first: a crash
// before the file is removed leaves an orphan file, which Sweep cleans up,
// never a row pointing at nothing.
func (s *SlideDecks) Remove(id int64) error {
	d, err := s.Get(id)
	if err != nil {
		return err
	}
	if err := s.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM slide_decks WHERE id = ?`, id)
		return err
	}); err != nil {
		return err
	}
	// Like Open: d.stored is the generated uuid name, not user input.
	if err := os.Remove(filepath.Join(s.dir, d.stored)); err != nil && !errors.Is(err, os.ErrNotExist) { //nolint:gosec // G703: stored is the generated uuid name, not user input
		return err
	}
	return nil
}

// Signature is what the personal pages' streams watch: which Slide Decks
// exist. The section refreshes when a deck is added or removed.
func (s *SlideDecks) Signature() string {
	decks, err := s.List()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, d := range decks {
		fmt.Fprintf(&b, "%d;", d.ID)
	}
	return b.String()
}

// Sweep removes every file in the slides directory that no Slide Deck points
// at: the files of a Reset, and any leftover of a crash between a write and a
// delete (best effort). A missing directory is fine.
func (s *SlideDecks) Sweep() error {
	decks, err := s.List()
	if err != nil {
		return err
	}
	return s.removeOrphans(keptDeckNames(decks))
}

// keptDeckNames is the set of files the Slide Decks point at.
func keptDeckNames(decks []SlideDeck) map[string]bool {
	keep := make(map[string]bool, len(decks))
	for _, d := range decks {
		keep[d.stored] = true
	}
	return keep
}

// removeOrphans deletes every file in the slides directory that no Slide
// Deck points at (best effort). A missing directory is fine.
func (s *SlideDecks) removeOrphans(keep map[string]bool) error {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		s.sweepFile(e, keep)
	}
	return nil
}

// sweepFile deletes one orphaned file. Best effort: a file that will not go
// is tried again at the next sweep. ReadDir yields plain names of the app's
// own files: no traversal.
func (s *SlideDecks) sweepFile(entry os.DirEntry, keep map[string]bool) {
	if entry.IsDir() || keep[entry.Name()] {
		return
	}
	_ = os.Remove(filepath.Join(s.dir, entry.Name())) //nolint:gosec // G703: e.Name() is a base name from ReadDir, never user input
}
