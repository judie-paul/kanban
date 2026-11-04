package web

import (
	"net/http"
	"time"

	"kanban/internal/apperr"
	"kanban/internal/middleware"
	"kanban/internal/users"
)

type authForm struct {
	Name  string
	Email string
	Next  string
	Error string
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "home", "Welcome", nil)
}

func (a *App) registerForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "register", "Create your account", authForm{})
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	form := authForm{Name: r.PostFormValue("name"), Email: r.PostFormValue("email")}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("password_confirm") {
		form.Error = "the two passwords do not match"
		a.render(w, r, http.StatusUnprocessableEntity, "register", "Create your account", form)
		return
	}
	u, err := a.auth.Register(r.Context(), form.Name, form.Email, password)
	if err != nil {
		if msg, ok := apperr.IsValidation(err); ok {
			form.Error = msg
			a.render(w, r, http.StatusUnprocessableEntity, "register", "Create your account", form)
			return
		}
		a.serverError(w, r, err)
		return
	}
	a.logger.Info("user registered", "user_id", u.ID)
	if err := a.startSession(w, r, u); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.setFlash(w, "success", "Welcome, "+u.Name+"! Create your first board to get started.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "login", "Log in", authForm{Next: r.URL.Query().Get("next")})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	form := authForm{Email: r.PostFormValue("email"), Next: r.PostFormValue("next")}
	u, err := a.auth.Authenticate(r.Context(), form.Email, r.PostFormValue("password"))
	if err != nil {
		if msg, ok := apperr.IsValidation(err); ok {
			a.logger.Warn("failed login", "email", users.NormalizeEmail(form.Email))
			form.Error = msg
			a.render(w, r, http.StatusUnauthorized, "login", "Log in", form)
			return
		}
		a.serverError(w, r, err)
		return
	}
	if err := a.startSession(w, r, u); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.logger.Info("user logged in", "user_id", u.ID)
	http.Redirect(w, r, safeRedirect(form.Next, "/dashboard"), http.StatusSeeOther)
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, u users.User) error {
	// Replace any existing session to prevent session fixation.
	if c, err := r.Cookie(middleware.SessionCookie); err == nil {
		a.auth.DeleteSession(r.Context(), c.Value)
	}
	token, expires, err := a.auth.CreateSession(r.Context(), u.ID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(middleware.SessionCookie); err == nil {
		if err := a.auth.DeleteSession(r.Context(), c.Value); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: middleware.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	a.setFlash(w, "success", "You have been logged out.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type profilePage struct {
	Profile    users.User
	BoardCount int
	TaskCount  int
	Assigned   int
}

func (a *App) profile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	p := profilePage{Profile: u}
	ctx := r.Context()
	a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM board_members WHERE user_id = ?`, u.ID).Scan(&p.BoardCount)
	a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE creator_id = ?`, u.ID).Scan(&p.TaskCount)
	a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks t JOIN columns c ON c.id = t.column_id
		WHERE t.assignee_id = ? AND c.is_done = 0`, u.ID).Scan(&p.Assigned)
	a.render(w, r, http.StatusOK, "profile", "Your profile", p)
}

func (a *App) updateProfile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := a.users.UpdateName(r.Context(), u.ID, r.PostFormValue("name")); err != nil {
		a.handleError(w, r, err, "/profile")
		return
	}
	a.setFlash(w, "success", "Your profile has been updated.")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	next := r.PostFormValue("new_password")
	if next != r.PostFormValue("new_password_confirm") {
		a.handleError(w, r, apperr.Invalid("the new passwords do not match"), "/profile")
		return
	}
	token := ""
	if c, err := r.Cookie(middleware.SessionCookie); err == nil {
		token = c.Value
	}
	err := a.auth.ChangePassword(r.Context(), u.ID, r.PostFormValue("current_password"), next, token)
	if err != nil {
		a.handleError(w, r, err, "/profile")
		return
	}
	a.setFlash(w, "success", "Your password has been changed. Other devices have been signed out.")
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}
