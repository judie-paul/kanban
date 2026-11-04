// Package web contains the HTTP layer: routing, handlers and template
// rendering. Handlers parse requests, call the domain services and render
// responses; business rules and permission checks live in the services.
package web

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"kanban/internal/activity"
	"kanban/internal/apperr"
	"kanban/internal/auth"
	"kanban/internal/boards"
	"kanban/internal/comments"
	"kanban/internal/middleware"
	"kanban/internal/notifications"
	"kanban/internal/tasks"
	"kanban/internal/users"
)

// Config holds the dependencies and settings of the web application.
type Config struct {
	DB            *sql.DB
	Logger        *slog.Logger
	TemplatesFS   fs.FS // must contain templates/{layouts,pages,partials}
	StaticFS      fs.FS // must contain static/
	SecureCookies bool  // set when serving over HTTPS
}

// App wires services to HTTP handlers.
type App struct {
	db            *sql.DB
	logger        *slog.Logger
	secureCookies bool
	templates     map[string]*template.Template
	staticFS      fs.FS

	users         *users.Service
	auth          *auth.Service
	boards        *boards.Service
	tasks         *tasks.Service
	comments      *comments.Service
	activity      *activity.Service
	notifications *notifications.Service
}

// New builds the application and parses all templates up front so template
// errors are discovered at startup rather than on first request.
func New(cfg Config) (*App, error) {
	tmpls, err := parseTemplates(cfg.TemplatesFS)
	if err != nil {
		return nil, err
	}
	u := &users.Service{DB: cfg.DB}
	b := &boards.Service{DB: cfg.DB}
	t := &tasks.Service{DB: cfg.DB, Boards: b}
	return &App{
		db:            cfg.DB,
		logger:        cfg.Logger,
		secureCookies: cfg.SecureCookies,
		templates:     tmpls,
		staticFS:      cfg.StaticFS,
		users:         u,
		auth:          auth.NewService(cfg.DB, u),
		boards:        b,
		tasks:         t,
		comments:      &comments.Service{DB: cfg.DB, Boards: b, Tasks: t},
		activity:      &activity.Service{DB: cfg.DB},
		notifications: &notifications.Service{DB: cfg.DB},
	}, nil
}

// Auth exposes the auth service (used by main for session cleanup and by tests).
func (a *App) Auth() *auth.Service { return a.auth }

// Routes returns the fully wrapped HTTP handler.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(a.staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Public pages.
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /register", a.registerForm)
	mux.HandleFunc("POST /register", a.register)
	mux.HandleFunc("GET /login", a.loginForm)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)

	// Authenticated pages.
	protected := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, middleware.RequireAuth(h))
	}
	protected("GET /profile", a.profile)
	protected("POST /profile", a.updateProfile)
	protected("POST /profile/password", a.changePassword)

	protected("GET /dashboard", a.dashboard)

	protected("POST /boards", a.createBoard)
	protected("GET /boards/{boardID}", a.showBoard)
	protected("GET /boards/{boardID}/settings", a.boardSettings)
	protected("GET /boards/{boardID}/activity", a.boardActivity)
	protected("POST /boards/{boardID}/update", a.updateBoard)
	protected("POST /boards/{boardID}/delete", a.deleteBoard)
	protected("POST /boards/{boardID}/leave", a.leaveBoard)
	protected("POST /boards/{boardID}/members", a.addMember)
	protected("POST /boards/{boardID}/members/{userID}/role", a.changeMemberRole)
	protected("POST /boards/{boardID}/members/{userID}/remove", a.removeMember)
	protected("POST /boards/{boardID}/columns", a.createColumn)
	protected("POST /boards/{boardID}/columns/{columnID}/update", a.updateColumn)
	protected("POST /boards/{boardID}/columns/{columnID}/reorder", a.reorderColumn)
	protected("POST /boards/{boardID}/columns/{columnID}/delete", a.deleteColumn)
	protected("POST /boards/{boardID}/labels", a.createLabel)
	protected("POST /boards/{boardID}/labels/{labelID}/delete", a.deleteLabel)
	protected("POST /boards/{boardID}/tasks", a.createTask)

	protected("GET /tasks/{taskID}", a.showTask)
	protected("POST /tasks/{taskID}/update", a.updateTask)
	protected("POST /tasks/{taskID}/move", a.moveTask)
	protected("POST /tasks/{taskID}/delete", a.deleteTask)
	protected("POST /tasks/{taskID}/comments", a.addComment)
	protected("POST /comments/{commentID}/update", a.updateComment)
	protected("POST /comments/{commentID}/delete", a.deleteComment)

	protected("GET /notifications", a.listNotifications)
	protected("POST /notifications/{notificationID}/read", a.markNotificationRead)
	protected("POST /notifications/read-all", a.markAllNotificationsRead)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		a.notFound(w, r)
	})

	return middleware.Chain(mux,
		middleware.Recover(a.logger),
		middleware.SecureHeaders,
		middleware.Authenticate(a.auth),
		middleware.Logging(a.logger),
		middleware.LimitBody(1<<20),
		middleware.CSRF(a.secureCookies, a.logger),
	)
}

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// ---- rendering ----------------------------------------------------------

// flash is a one-time message shown after a redirect.
type flash struct {
	Kind    string // "success" or "error"
	Message string
}

// pageData is passed to every template.
type pageData struct {
	Title       string
	User        *users.User
	CSRFToken   string
	Flash       *flash
	UnreadCount int
	Path        string
	Data        any
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any) {
	tmpl, ok := a.templates[page]
	if !ok {
		a.serverError(w, r, errors.New("unknown template "+page))
		return
	}
	pd := pageData{
		Title:     title,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Flash:     a.popFlash(w, r),
		Path:      r.URL.RequestURI(),
		Data:      data,
	}
	if u, ok := middleware.UserFrom(r.Context()); ok {
		pd.User = &u
		if n, err := a.notifications.UnreadCount(r.Context(), u.ID); err == nil {
			pd.UnreadCount = n
		}
	}
	// Render into a buffer first so a template error never produces a
	// half-written page.
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", pd); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *App) setFlash(w http.ResponseWriter, kind, msg string) {
	v := base64.RawURLEncoding.EncodeToString([]byte(kind + "|" + msg))
	http.SetCookie(w, &http.Cookie{
		Name: "kanban_flash", Value: v, Path: "/", HttpOnly: true,
		Secure: a.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 60,
	})
}

func (a *App) popFlash(w http.ResponseWriter, r *http.Request) *flash {
	c, err := r.Cookie("kanban_flash")
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: "kanban_flash", Value: "", Path: "/", MaxAge: -1})
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	kind, msg, ok := strings.Cut(string(raw), "|")
	if !ok || (kind != "success" && kind != "error") {
		return nil
	}
	return &flash{Kind: kind, Message: msg}
}

// ---- errors -------------------------------------------------------------

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("internal error", "err", err, "method", r.Method, "path", r.URL.Path)
	http.Error(w, "Something went wrong on our side. Please try again.", http.StatusInternalServerError)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusNotFound, "error", "Not found", errorPage{
		Code: 404, Heading: "Page not found",
		Message: "The page you are looking for does not exist, or you do not have access to it.",
	})
}

type errorPage struct {
	Code    int
	Heading string
	Message string
}

// handleError maps service errors to responses. Validation and permission
// errors are reported to the user via a flash message and a redirect back to
// `back`; unknown errors are logged and hidden behind a generic 500.
func (a *App) handleError(w http.ResponseWriter, r *http.Request, err error, back string) {
	if wantsJSON(r) {
		status, msg := http.StatusInternalServerError, "Something went wrong."
		switch {
		case errors.Is(err, apperr.ErrNotFound):
			status, msg = http.StatusNotFound, "Not found."
		case errors.Is(err, apperr.ErrForbidden):
			status, msg = http.StatusForbidden, err.Error()
		default:
			if m, ok := apperr.IsValidation(err); ok {
				status, msg = http.StatusUnprocessableEntity, m
			} else {
				a.logger.Error("internal error", "err", err, "path", r.URL.Path)
			}
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": msg})
		return
	}
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		a.notFound(w, r)
	case errors.Is(err, apperr.ErrForbidden):
		a.setFlash(w, "error", err.Error())
		http.Redirect(w, r, back, http.StatusSeeOther)
	default:
		if msg, ok := apperr.IsValidation(err); ok {
			a.setFlash(w, "error", msg)
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		a.serverError(w, r, err)
	}
}

// ---- small helpers ------------------------------------------------------

func currentUser(r *http.Request) users.User {
	u, _ := middleware.UserFrom(r.Context())
	return u
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func formID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue(name)), 10, 64)
	return id
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// safeRedirect only allows local paths, preventing open redirects.
func safeRedirect(target, fallback string) string {
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return fallback
	}
	if u, err := url.Parse(target); err != nil || u.Host != "" || u.Scheme != "" {
		return fallback
	}
	return target
}
