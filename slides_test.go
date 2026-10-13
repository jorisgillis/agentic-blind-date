package main

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
)

var samplePDF = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")

// uploadSlides posts the Host's upload form. An empty filename leaves the file
// part out.
func uploadSlides(t *testing.T, srv *testSrv, title, speaker, filename string, content []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("title", title)
	mw.WriteField("speaker", speaker)
	if filename != "" {
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(content)
	}
	mw.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/admin/slides", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// addDeck stores a Slide Deck directly, as test setup.
func addDeck(t *testing.T, deps *testDeps, title, speaker string) *SlideDeck {
	t.Helper()
	deck, err := deps.slides.Add(title, speaker, "slides.pdf", bytes.NewReader(samplePDF))
	if err != nil {
		t.Fatal(err)
	}
	return deck
}

// filesOnTheVolume lists the PDFs stored for the Slide Decks.
func filesOnTheVolume(t *testing.T, deps *testDeps) []string {
	t.Helper()
	entries, err := os.ReadDir(deps.slides.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func deckPath(deck *SlideDeck, suffix string) string {
	return "/slides/" + strconv.FormatInt(deck.ID, 10) + suffix
}

func TestSlideDecks_TheHostUploadsAPDF(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	resp := uploadSlides(t, srv, "  Agents in production ", " Ada Lovelace ", "My Talk.PDF", samplePDF)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/slides") {
		t.Fatalf("a valid upload: want a redirect to the slides page, got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	decks, err := deps.slides.List()
	if err != nil || len(decks) != 1 {
		t.Fatalf("one deck should be stored, got %+v (%v)", decks, err)
	}
	if d := decks[0]; d.Title != "Agents in production" || d.Speaker != "Ada Lovelace" || d.Filename != "My Talk.PDF" || d.Size != int64(len(samplePDF)) {
		t.Errorf("the deck should keep the trimmed title and speaker, the filename and the size, got %+v", d)
	}
	files := filesOnTheVolume(t, deps)
	if len(files) != 1 || strings.Contains(files[0], "Talk") || !strings.HasSuffix(files[0], ".pdf") {
		t.Errorf("the PDF should be stored under a generated name, got %v", files)
	}

	page := readBody(t, get(t, srv, "/admin/slides"))
	for _, want := range []string{"Agents in production", "Ada Lovelace", `action="/admin/slides/remove"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the slides page should contain %q", want)
		}
	}
}

func TestSlideDecks_TheHostPageDoesNotReloadItself(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	page := readBody(t, get(t, srv, "/admin/slides"))
	if strings.Contains(page, `http-equiv="refresh"`) {
		t.Error("a reload would wipe the chosen file")
	}
	if !strings.Contains(readBody(t, get(t, srv, "/admin")), `href="/admin/slides"`) {
		t.Error("the host console should link to the slides page")
	}
}

func TestSlideDecks_RejectedUploadsStoreNothing(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	for _, tc := range []struct {
		name                 string
		title, speaker, file string
		content              []byte
		wantReason           string
	}{
		{"no title", " ", "Ada", "a.pdf", samplePDF, "title"},
		{"no speaker", "Talk", " ", "a.pdf", samplePDF, "speaker"},
		{"no file", "Talk", "Ada", "", nil, "Choose a PDF"},
		{"not a .pdf name", "Talk", "Ada", "a.txt", samplePDF, "must be a PDF"},
		{"not PDF content", "Talk", "Ada", "a.pdf", []byte("<html>surprise</html>"), "not a PDF"},
		{"empty file", "Talk", "Ada", "a.pdf", nil, "not a PDF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := uploadSlides(t, srv, tc.title, tc.speaker, tc.file, tc.content)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("want 400, got %d", resp.StatusCode)
			}
			if body := readBody(t, resp); !strings.Contains(body, tc.wantReason) {
				t.Errorf("the page should say why (%q)", tc.wantReason)
			}
			if decks, _ := deps.slides.List(); len(decks) != 0 {
				t.Errorf("nothing should be stored, got %+v", decks)
			}
			if files := filesOnTheVolume(t, deps); len(files) != 0 {
				t.Errorf("no file should be left behind, got %v", files)
			}
		})
	}
}

func TestSlideDecks_UploadsOverTheCapAreRejected(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	defer func(old int64) { maxSlideDeckBytes = old }(maxSlideDeckBytes)
	maxSlideDeckBytes = 1024

	// Just over the cap: the file is refused while it is stored.
	over := append(append([]byte{}, samplePDF...), bytes.Repeat([]byte("x"), 1024)...)
	// Far over it: the request is refused while it is read.
	far := append(append([]byte{}, samplePDF...), bytes.Repeat([]byte("x"), 2<<20)...)

	for name, content := range map[string][]byte{"just over": over, "far over": far} {
		resp := uploadSlides(t, srv, "Talk", "Ada", "a.pdf", content)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s the cap: want 400, got %d", name, resp.StatusCode)
		}
		if body := readBody(t, resp); !strings.Contains(body, "too large") {
			t.Errorf("%s the cap: the page should say the file is too large", name)
		}
	}
	if decks, _ := deps.slides.List(); len(decks) != 0 {
		t.Errorf("nothing should be stored, got %+v", decks)
	}
	if files := filesOnTheVolume(t, deps); len(files) != 0 {
		t.Errorf("no file should be left behind, got %v", files)
	}

	if resp := uploadSlides(t, srv, "Talk", "Ada", "a.pdf", samplePDF); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("a file within the cap: want a redirect, got %d", resp.StatusCode)
	}
}

func TestSlideDecks_AFailedWriteIsAServerErrorAndLeavesNoFile(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	breakOnFault(t, deps.db, "slide_insert")

	if resp := uploadSlides(t, srv, "Talk", "Ada", "a.pdf", samplePDF); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("a write failure: want 500, got %d", resp.StatusCode)
	}
	if files := filesOnTheVolume(t, deps); len(files) != 0 {
		t.Errorf("no file should be left behind, got %v", files)
	}
}

func TestSlideDecks_TheHostRemovesADeck(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	keep := addDeck(t, deps, "Keep", "Ada")
	drop := addDeck(t, deps, "Drop", "Grace")

	if resp := post(t, srv, "/admin/slides/remove", url.Values{"id": {"nope"}}); resp.StatusCode != 400 {
		t.Errorf("a bad id: want 400, got %d", resp.StatusCode)
	}
	if resp := post(t, srv, "/admin/slides/remove", url.Values{"id": {"999"}}); resp.StatusCode != 404 {
		t.Errorf("an unknown deck: want 404, got %d", resp.StatusCode)
	}

	restore := failWrites(t, deps.db, "slide_delete")
	if resp := post(t, srv, "/admin/slides/remove", url.Values{"id": {strconv.FormatInt(drop.ID, 10)}}); resp.StatusCode != 500 {
		t.Errorf("a write failure: want 500, got %d", resp.StatusCode)
	}
	if files := filesOnTheVolume(t, deps); len(files) != 2 {
		t.Errorf("a failed removal should leave both files, got %v", files)
	}
	restore()

	if resp := post(t, srv, "/admin/slides/remove", url.Values{"id": {strconv.FormatInt(drop.ID, 10)}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("removing a deck: want a redirect, got %d", resp.StatusCode)
	}
	decks, _ := deps.slides.List()
	if len(decks) != 1 || decks[0].ID != keep.ID {
		t.Errorf("only the other deck should remain, got %+v", decks)
	}
	if files := filesOnTheVolume(t, deps); len(files) != 1 {
		t.Errorf("the removed deck's file should be gone, got %v", files)
	}
	if resp := get(t, srv, deckPath(drop, "")); resp.StatusCode != 404 {
		t.Errorf("a removed deck's link: want 404, got %d", resp.StatusCode)
	}
}

func TestSlideDecks_ParticipantsSeeThemInEveryPhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	pair(t, deps.db, "a", "b")

	// Without decks the section is there, and empty.
	if body := readBody(t, get(t, srv, "/user/wait/c")); !strings.Contains(body, `id="slides"`) || strings.Contains(body, "📑") {
		t.Errorf("the wait page without decks should carry an empty slides section")
	}

	first := addDeck(t, deps, "Agents in production", "Ada Lovelace")
	second := addDeck(t, deps, "Evals that work", "Grace Hopper")

	check := func(name, path string, phase Phase) {
		t.Helper()
		body := readBody(t, get(t, srv, path))
		for _, want := range []string{
			"Agents in production", "Ada Lovelace", "Evals that work", "Grace Hopper",
			`href="` + deckPath(first, "") + `"`, `href="` + deckPath(first, "/download") + `"`,
			`href="` + deckPath(second, "") + `"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s during %s should contain %q", name, phase, want)
			}
		}
		if strings.Index(body, "Agents in production") > strings.Index(body, "Evals that work") {
			t.Errorf("%s during %s should list the decks in upload order", name, phase)
		}
	}

	check("the wait page", "/user/wait/c", PhaseOnboarding)
	for _, phase := range []Phase{PhaseSocial, PhaseShowTell, PhaseClosed} {
		if err := deps.events.Advance(phase); err != nil {
			t.Fatal(err)
		}
		check("the wait page", "/user/wait/c", phase)
		check("the Match page", "/user/match/a", phase)
	}

	if body := readBody(t, get(t, srv, "/user/slides/c")); !strings.Contains(body, "Evals that work") {
		t.Errorf("the slides fragment should list the decks")
	}
	if resp := get(t, srv, "/user/slides/nobody"); resp.StatusCode != 404 {
		t.Errorf("an unknown participant: want 404, got %d", resp.StatusCode)
	}
}

func TestSlideDecks_TheLinksServeThePDF(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deck, err := deps.slides.Add("Talk", "Ada", "My Talk.pdf", bytes.NewReader(samplePDF))
	if err != nil {
		t.Fatal(err)
	}

	for suffix, disposition := range map[string]string{"": "inline", "/download": "attachment"} {
		resp := get(t, srv, deckPath(deck, suffix))
		if resp.StatusCode != 200 {
			t.Fatalf("%q: want 200, got %d", suffix, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("%q: Content-Type: want application/pdf, got %q", suffix, got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%q: X-Content-Type-Options: want nosniff, got %q", suffix, got)
		}
		if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, disposition) || !strings.Contains(got, "My Talk.pdf") {
			t.Errorf("%q: Content-Disposition: want %s with the filename, got %q", suffix, disposition, got)
		}
		b, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(b, samplePDF) {
			t.Errorf("%q: the body should be the uploaded bytes", suffix)
		}
	}

	for _, path := range []string{"/slides/999", "/slides/abc", "/slides/999/download", "/slides/abc/download"} {
		if resp := get(t, srv, path); resp.StatusCode != 404 {
			t.Errorf("%s: want 404, got %d", path, resp.StatusCode)
		}
	}

	// A deck whose file went missing is not found, not a server error.
	for _, f := range filesOnTheVolume(t, deps) {
		os.Remove(filepath.Join(deps.slides.dir, f))
	}
	if resp := get(t, srv, deckPath(deck, "")); resp.StatusCode != 404 {
		t.Errorf("a missing file: want 404, got %d", resp.StatusCode)
	}
}

func TestSlideDecks_PersonalStreamsRefreshWhenADeckIsAddedOrRemoved(t *testing.T) {
	for _, path := range []string{"/user/wait-stream/w", "/user/match-stream/w"} {
		t.Run(path, func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			seed(t, deps.db, "w", "W", "ready")
			s := open(t, srv, path)
			sentNothingNew(t, s, 1, "", "nothing changed")

			deck := addDeck(t, deps, "Talk", "Ada")
			waitFor(t, s, "event: refresh")

			before := s.String()
			if err := deps.slides.Remove(deck.ID); err != nil {
				t.Fatal(err)
			}
			eventually(t, "the removal to refresh the page", func() bool {
				return len(s.String()) > len(before)
			})
		})
	}
}

func TestSlideDecks_AResetDeletesThemAndTheirFiles(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	addDeck(t, deps, "One", "Ada")
	addDeck(t, deps, "Two", "Grace")

	restore := failWrites(t, deps.db, "reset_slide_decks")
	if resp := post(t, srv, "/admin/reset", url.Values{}); resp.StatusCode != 500 {
		t.Fatalf("a failed reset: want 500, got %d", resp.StatusCode)
	}
	if decks, _ := deps.slides.List(); len(decks) != 2 || len(filesOnTheVolume(t, deps)) != 2 {
		t.Fatalf("a failed reset should leave everything as it was")
	}
	restore()

	if resp := post(t, srv, "/admin/reset", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset: want a redirect, got %d", resp.StatusCode)
	}
	if decks, _ := deps.slides.List(); len(decks) != 0 {
		t.Errorf("a reset should delete the decks, got %+v", decks)
	}
	if files := filesOnTheVolume(t, deps); len(files) != 0 {
		t.Errorf("a reset should delete the files, got %v", files)
	}
}

func TestSlideDecks_SweepRemovesFilesNoDeckPointsAt(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	deps.slides.dir = filepath.Join(deps.slides.dir, "slides") // not created yet

	if err := deps.slides.Sweep(); err != nil {
		t.Errorf("sweeping a volume without a slides directory: %v", err)
	}

	deck := addDeck(t, deps, "Kept", "Ada")
	orphan := filepath.Join(deps.slides.dir, "orphan.pdf")
	if err := os.WriteFile(orphan, samplePDF, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deps.slides.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the orphan file should be removed, stat: %v", err)
	}
	if _, f, err := deps.slides.Open(deck.ID); err != nil {
		t.Errorf("the deck's own file should stay: %v", err)
	} else {
		f.Close()
	}
}

func TestSlideDecks_AreNotOnTheOtherSurfaces(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	addDeck(t, deps, "Secret Slides Title", "Speaker Name")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/bigscreen", "/bigscreen/graph-data", "/recap", "/admin/export", "/admin/export?format=md"} {
		resp := get(t, srv, path)
		if resp.StatusCode != 200 {
			t.Errorf("%s: want 200, got %d", path, resp.StatusCode)
			continue
		}
		if body := readBody(t, resp); strings.Contains(body, "Secret Slides Title") || strings.Contains(body, "Speaker Name") {
			t.Errorf("%s should not show the Slide Decks", path)
		}
	}
	recap := readBody(t, get(t, srv, "/user/recap-link/a"))
	if strings.Contains(recap, "Secret Slides Title") {
		t.Errorf("the recap link should not show the Slide Decks")
	}
}

func TestSlideDecks_DatabaseReadFailuresAreReported(t *testing.T) {
	for _, tag := range []string{"slide_read", "slide_query_read"} {
		t.Run(tag, func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			seed(t, deps.db, "a", "The Gopher", "ready")
			deck := addDeck(t, deps, "A talk title", "Ada")
			breakOnFault(t, deps.db, tag)

			if _, err := deps.slides.List(); err == nil {
				t.Error("List should report the failure")
			}
			if _, err := deps.slides.Get(deck.ID); err == nil || errors.Is(err, ErrSlideDeckNotFound) {
				t.Errorf("Get should report the failure, got %v", err)
			}
			if _, _, err := deps.slides.Open(deck.ID); err == nil {
				t.Error("Open should report the failure")
			}
			if err := deps.slides.Remove(deck.ID); err == nil {
				t.Error("Remove should report the failure")
			}
			if err := deps.slides.Sweep(); err == nil {
				t.Error("Sweep should report the failure, not delete files it cannot account for")
			}
			if sig := deps.slides.Signature(); sig != "" {
				t.Errorf("the signature should be empty, got %q", sig)
			}

			// The pages carry on without decks; the file link is a server error.
			for _, path := range []string{"/user/slides/a", "/admin/slides"} {
				resp := get(t, srv, path)
				if resp.StatusCode != 200 {
					t.Errorf("%s: want 200, got %d", path, resp.StatusCode)
				}
				if body := readBody(t, resp); strings.Contains(body, "A talk title") {
					t.Errorf("%s should show no decks while they cannot be read", path)
				}
			}
			if resp := get(t, srv, deckPath(deck, "")); resp.StatusCode != 500 {
				t.Errorf("the deck link: want 500, got %d", resp.StatusCode)
			}
		})
	}
}

func TestSlideDecks_AVolumeThatCannotBeUsed(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	good := deps.slides.dir
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// The slides directory cannot be created or read: it is a file.
	deps.slides.dir = blocker
	if _, err := deps.slides.Add("Talk", "Ada", "a.pdf", bytes.NewReader(samplePDF)); err == nil {
		t.Error("Add should report a directory it cannot create")
	}
	if err := deps.slides.Sweep(); err == nil {
		t.Error("Sweep should report a directory it cannot read")
	}
	if resp := uploadSlides(t, srv, "Talk", "Ada", "a.pdf", samplePDF); resp.StatusCode != 500 {
		t.Errorf("an upload the volume cannot take: want 500, got %d", resp.StatusCode)
	}
	// A reset still succeeds: the rows are gone, the file cleanup is logged.
	if resp := post(t, srv, "/admin/reset", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("a reset with an unreadable slides directory: want a redirect, got %d", resp.StatusCode)
	}
	deps.slides.dir = good

	// The directory exists but nothing can be written into it.
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(good, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(good, 0o755)
	if _, err := deps.slides.Add("Talk", "Ada", "a.pdf", bytes.NewReader(samplePDF)); err == nil {
		t.Error("Add should report a directory it cannot write to")
	}
}

func TestSlideDecks_AFileThatCannotBeOpenedOrRemoved(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deck := addDeck(t, deps, "Talk", "Ada")
	// A stored name no file system accepts: the file is neither missing nor usable.
	if _, err := deps.db.db.Exec(`UPDATE slide_decks SET stored_name = ? WHERE id = ?`, strings.Repeat("a", 300), deck.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := deps.slides.Open(deck.ID); err == nil || errors.Is(err, ErrSlideDeckNotFound) {
		t.Errorf("Open should report the failure, got %v", err)
	}
	if resp := get(t, srv, deckPath(deck, "")); resp.StatusCode != 500 {
		t.Errorf("the deck link: want 500, got %d", resp.StatusCode)
	}
	if resp := post(t, srv, "/admin/slides/remove", url.Values{"id": {strconv.FormatInt(deck.ID, 10)}}); resp.StatusCode != 500 {
		t.Errorf("removing a deck whose file cannot be removed: want 500, got %d", resp.StatusCode)
	}
}

func TestSlideDecks_AnUploadThatBreaksOffLeavesNothing(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	broken := io.MultiReader(strings.NewReader(pdfMagic), iotest.ErrReader(errors.New("connection reset")))

	_, err := deps.slides.Add("Talk", "Ada", "a.pdf", broken)
	var invalid *InvalidSlideDeckError
	if err == nil || errors.As(err, &invalid) {
		t.Errorf("a broken upload is a server error, not an invalid deck, got %v", err)
	}
	if files := filesOnTheVolume(t, deps); len(files) != 0 {
		t.Errorf("no file should be left behind, got %v", files)
	}
}

func TestSlideDecks_AnUploadThatIsNotAFormIsRejected(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	resp := post(t, srv, "/admin/slides", url.Values{"title": {"Talk"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "could not be read") {
		t.Errorf("the page should say the upload could not be read")
	}
}

func TestSlideDecks_TheHostPageShowsLargeDecksInMegabytes(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	big := append(append([]byte{}, samplePDF...), bytes.Repeat([]byte("x"), 1<<20)...)
	if _, err := deps.slides.Add("Big", "Ada", "big.pdf", bytes.NewReader(big)); err != nil {
		t.Fatal(err)
	}
	addDeck(t, deps, "Small", "Grace")

	body := readBody(t, get(t, srv, "/admin/slides"))
	for _, want := range []string{"1.0 MB", "1 KB"} {
		if !strings.Contains(body, want) {
			t.Errorf("the slides page should show a size of %q", want)
		}
	}
}
