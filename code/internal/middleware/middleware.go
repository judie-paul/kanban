// Package middleware contains the HTTP middleware chain: request logging,
// panic recovery, security headers, CSRF protection and authentication.
package middleware

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"kanban/internal/auth"
	"kanban/internal/users"
)

// Cookie names.
const (
	SessionCookie = "kanban_session"
	CSRFCookie    = "kanban_csrf"
	CSRFField     = "csrf_token"
	CSRFHeader    = "X-CSRF-Token"
)

type ctxKey int

const (
	userKey ctxKey = iota
	csrfKey
)

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (users.User, bool) {
	u, ok := ctx.Value(userKey).(users.User)
	return u, ok
}

// CSRFTokenFrom returns the CSRF token to embed in forms.
func CSRFTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey).(string)
	return t
}

// statusRecorder captures the status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Logging logs one structured line per request.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if u, ok := UserFrom(r.Context()); ok {
				attrs = append(attrs, "user_id", u.ID)
			}
			logger.Info("request", attrs...)
		})
	}
}

// Recover turns panics into 500 responses without leaking details.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler {
						panic(p)
					}
					logger.Error("panic serving request", "panic", p, "path", r.URL.Path)
					w.Header().Set("Connection", "close")
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecureHeaders sets conservative browser security headers.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// Authenticate loads the user from the session cookie (if present and valid)
// into the request context. It never rejects a request; see RequireAuth.
func Authenticate(a *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookie)
			if err == nil && c.Value != "" {
				if u, err := a.UserForSession(r.Context(), c.Value); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userKey, u))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth redirects anonymous users to the login page (or returns 401 for
// fetch/JSON requests).
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); !ok {
			if r.Header.Get("Accept") == "application/json" {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}
			target := "/login"
			if r.Method == http.MethodGet {
				target += "?next=" + url.QueryEscape(r.URL.RequestURI())
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRF implements the double-submit-cookie pattern: every visitor receives a
// random token in an HttpOnly, SameSite=Lax cookie; the server embeds the same
// token in every form (and in a <meta> tag for fetch requests). Any
// state-changing request must echo the token back in the csrf_token form
// field or the X-CSRF-Token header, which a cross-site attacker cannot read.
func CSRF(secureCookies bool, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""
			if c, err := r.Cookie(CSRFCookie); err == nil && len(c.Value) >= 32 {
				token = c.Value
			} else {
				t, err := auth.RandomToken()
				if err != nil {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				token = t
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookie,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					Secure:   secureCookies,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   int((30 * 24 * time.Hour).Seconds()),
				})
			}

			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				sent := r.Header.Get(CSRFHeader)
				if sent == "" {
					sent = r.PostFormValue(CSRFField)
				}
				if sent == "" || subtle.ConstantTimeCompare([]byte(sent), []byte(token)) != 1 {
					logger.Warn("csrf token mismatch", "path", r.URL.Path)
					http.Error(w, "Your session form has expired. Please go back, refresh the page and try again.", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey, token)))
		})
	}
}

// Chain applies middleware so that the first argument is the outermost.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
