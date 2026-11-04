package web_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	kanban "kanban"
	"kanban/internal/testdb"
	"kanban/internal/web"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := testdb.New(t)
	app, err := web.New(web.Config{
		DB:          db,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		TemplatesFS: kanban.Templates,
		StaticFS:    kanban.Static,
	})
	if err != nil {
		t.Fatal(err)
	}
	app.Auth().BcryptCost = bcrypt.MinCost
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	return srv
}

// client is a browser-like test client with its own cookie jar.
type client struct {
	t    *testing.T
	srv  *httptest.Server
	http *http.Client
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: srv, http: &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // inspect redirects ourselves
		},
	}}
}

func (c *client) get(path string) (int, string, http.Header) {
	c.t.Helper()
	resp, err := c.http.Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

var csrfRe = regexp.MustCompile(`name="csrf-token" content="([^"]+)"`)

func (c *client) csrf() string {
	c.t.Helper()
	_, body, _ := c.get("/login") // anonymous visitors
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		_, body, _ = c.get("/profile") // logged-in users are redirected away from /login
		m = csrfRe.FindStringSubmatch(body)
	}
	if m == nil {
		c.t.Fatal("csrf token not found")
	}
	return m[1]
}

func (c *client) post(path string, form url.Values) (int, string, http.Header) {
	c.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	if form.Get("csrf_token") == "" {
		form.Set("csrf_token", c.csrf())
	}
	resp, err := c.http.PostForm(c.srv.URL+path, form)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func (c *client) register(name, email string) {
	c.t.Helper()
	code, body, h := c.post("/register", url.Values{
		"name": {name}, "email": {email}, "password": {"password123"}, "password_confirm": {"password123"},
	})
	if code != http.StatusSeeOther || h.Get("Location") != "/dashboard" {
		c.t.Fatalf("register %s: status %d location %q body %s", email, code, h.Get("Location"), body)
	}
}

func TestProtectedRoutesRequireLogin(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	for _, p := range []string{"/dashboard", "/profile", "/notifications", "/boards/1", "/tasks/1"} {
		code, _, h := c.get(p)
		if code != http.StatusSeeOther || !strings.HasPrefix(h.Get("Location"), "/login") {
			t.Errorf("GET %s: status %d location %q; want redirect to /login", p, code, h.Get("Location"))
		}
	}
	code, _, h := c.post("/boards", url.Values{"name": {"x"}})
	if code != http.StatusSeeOther || !strings.HasPrefix(h.Get("Location"), "/login") {
		t.Errorf("POST /boards anonymously: status %d", code)
	}
}

func TestCSRFTokenRequired(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	c.register("Alice", "alice@example.com")
	resp, err := c.http.PostForm(srv.URL+"/boards", url.Values{"name": {"No token"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF token: status %d, want 403", resp.StatusCode)
	}
	code, _, _ := c.post("/boards", url.Values{"name": {"Bad token"}, "csrf_token": {"forged-token-forged-token-forged-token"}})
	if code != http.StatusForbidden {
		t.Fatalf("POST with wrong CSRF token: status %d, want 403", code)
	}
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	c.register("Alice Johnson", "alice@example.com")

	code, body, _ := c.get("/dashboard")
	if code != http.StatusOK || !strings.Contains(body, "Hello, Alice Johnson") {
		t.Fatalf("dashboard after register: %d", code)
	}

	// Duplicate email is rejected with a helpful message.
	other := newClient(t, srv)
	code, body, _ = other.post("/register", url.Values{
		"name": {"Imposter"}, "email": {"ALICE@example.com"}, "password": {"password123"}, "password_confirm": {"password123"},
	})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "already exists") {
		t.Fatalf("duplicate register: %d", code)
	}

	// Logout ends the session.
	code, _, h := c.post("/logout", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("logout: %d %q", code, h.Get("Location"))
	}
	if code, _, _ := c.get("/dashboard"); code != http.StatusSeeOther {
		t.Fatalf("dashboard after logout: %d, want redirect", code)
	}

	// Wrong password fails, right password works and honours ?next.
	code, body, _ = c.post("/login", url.Values{"email": {"alice@example.com"}, "password": {"nope-nope"}})
	if code != http.StatusUnauthorized || !strings.Contains(body, "incorrect email or password") {
		t.Fatalf("bad login: %d", code)
	}
	code, _, h = c.post("/login", url.Values{"email": {"alice@example.com"}, "password": {"password123"}, "next": {"/profile"}})
	if code != http.StatusSeeOther || h.Get("Location") != "/profile" {
		t.Fatalf("login: %d %q", code, h.Get("Location"))
	}
	// Open redirects are refused.
	c.post("/logout", nil)
	_, _, h = c.post("/login", url.Values{"email": {"alice@example.com"}, "password": {"password123"}, "next": {"//evil.example"}})
	if h.Get("Location") != "/dashboard" {
		t.Fatalf("open redirect allowed: %q", h.Get("Location"))
	}
}

var idRe = regexp.MustCompile(`/boards/(\d+)`)

func TestEndToEndBoardTaskFlow(t *testing.T) {
	srv := newServer(t)
	alice := newClient(t, srv)
	bob := newClient(t, srv)
	mallory := newClient(t, srv)
	alice.register("Alice Johnson", "alice@example.com")
	bob.register("Bob Smith", "bob@example.com")
	mallory.register("Mallory", "mallory@example.com")

	// Alice creates a board.
	code, _, h := alice.post("/boards", url.Values{"name": {"Launch"}, "description": {"Q4 launch"}})
	if code != http.StatusSeeOther {
		t.Fatalf("create board: %d", code)
	}
	boardURL := h.Get("Location")
	boardID := idRe.FindStringSubmatch(boardURL)[1]

	// Mallory cannot see it.
	if code, _, _ := mallory.get(boardURL); code != http.StatusNotFound {
		t.Fatalf("non-member GET board: %d, want 404", code)
	}

	// Alice adds Bob.
	code, _, _ = alice.post(boardURL+"/members", url.Values{"email": {"bob@example.com"}, "role": {"member"}})
	if code != http.StatusSeeOther {
		t.Fatalf("add member: %d", code)
	}

	// Find the column IDs from the board page.
	_, body, _ := alice.get(boardURL)
	cols := regexp.MustCompile(`data-column-id="(\d+)"`).FindAllStringSubmatch(body, -1)
	if len(cols) != 5 {
		t.Fatalf("found %d columns on board page", len(cols))
	}
	todo, inProgress := cols[1][1], cols[2][1]

	// Alice creates a task assigned to Bob. A forged creator field is ignored.
	code, _, h = alice.post(boardURL+"/tasks", url.Values{
		"column_id": {todo}, "title": {"Implement user authentication"}, "priority": {"medium"},
		"assignee_id": {"2"}, "creator_id": {"3"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("create task: %d", code)
	}
	taskID := regexp.MustCompile(`task=(\d+)`).FindStringSubmatch(h.Get("Location"))[1]

	// Bob moves it via the JSON (drag-and-drop) endpoint.
	csrf := bob.csrf()
	req, _ := http.NewRequest("POST", srv.URL+"/tasks/"+taskID+"/move",
		strings.NewReader(url.Values{"column_id": {inProgress}, "position": {"0"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := bob.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("json move: %d %s", resp.StatusCode, b)
	}

	// Bob changes priority via the edit form, then comments.
	code, _, _ = bob.post("/tasks/"+taskID+"/update", url.Values{
		"title": {"Implement user authentication"}, "priority": {"high"}, "assignee_id": {"2"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("update: %d", code)
	}
	bob.post("/tasks/"+taskID+"/comments", url.Values{"content": {"Authentication is now implemented."}})

	// Mallory can't touch the task.
	if code, _, _ := mallory.post("/tasks/"+taskID+"/update", url.Values{"title": {"pwned"}}); code != http.StatusNotFound {
		t.Fatalf("non-member update: %d, want 404", code)
	}

	// The task panel shows creator, assignee and the attributed history.
	_, body, _ = alice.get(fmt.Sprintf("/boards/%s?task=%s", boardID, taskID))
	for _, want := range []string{
		"Posted by", "Alice Johnson", "Bob Smith",
		"<strong>Alice Johnson</strong> created this task in To Do",
		"<strong>Alice Johnson</strong> assigned this task to Bob Smith",
		"<strong>Bob Smith</strong> moved this task from To Do to In Progress",
		"<strong>Bob Smith</strong> changed priority from Medium to High",
		"<strong>Bob Smith</strong> commented: “Authentication is now implemented.”",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task panel missing %q", want)
		}
	}

	// Every other page renders for a logged-in member.
	for _, p := range []string{"/dashboard", "/profile", "/notifications", boardURL + "/settings", boardURL + "/activity",
		boardURL + "?q=auth&priority=high&sort=due&due=week&assignee=-1"} {
		if code, body, _ := bob.get(p); code != http.StatusOK {
			t.Errorf("GET %s: %d %s", p, code, body)
		}
	}
	// Alice was notified that Bob changed her task.
	_, body, _ = alice.get("/notifications")
	if !strings.Contains(body, "updated your task") {
		t.Error("creator notification missing")
	}
	// /tasks/{id} redirects to the board panel.
	if code, _, h := bob.get("/tasks/" + taskID); code != http.StatusSeeOther || !strings.Contains(h.Get("Location"), "task="+taskID) {
		t.Errorf("GET /tasks/id: %d %q", code, h.Get("Location"))
	}
}

func TestStaticAndSecurityHeaders(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	for _, p := range []string{"/static/css/app.css", "/static/js/app.js", "/static/images/logo.svg"} {
		if code, _, _ := c.get(p); code != http.StatusOK {
			t.Errorf("GET %s: %d", p, code)
		}
	}
	_, _, h := c.get("/")
	if h.Get("Content-Security-Policy") == "" || h.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing")
	}
	if code, _, _ := c.get("/no-such-page"); code != http.StatusNotFound {
		t.Errorf("unknown page: %d", code)
	}
}
