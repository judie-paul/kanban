package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"time"

	"kanban/internal/boards"
	"kanban/internal/database"
	"kanban/internal/tasks"
	"kanban/internal/users"
)

// parseTemplates builds one template set per page: the base layout, all
// partials, and the page itself (which defines "title" and "content").
func parseTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	pages, err := fs.Glob(fsys, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	partials, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	out := map[string]*template.Template{}
	for _, page := range pages {
		name := strings.TrimSuffix(path.Base(page), ".html")
		files := append([]string{"templates/layouts/base.html"}, partials...)
		files = append(files, page)
		t, err := template.New(name).Funcs(templateFuncs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		out[name] = t
	}
	return out, nil
}

var templateFuncs = template.FuncMap{
	// datetime renders a UTC timestamp; app.js converts it to local time.
	"datetime": func(t database.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("Jan 2, 2006, 3:04 PM") + " UTC"
	},
	"date": func(t database.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("January 2, 2006")
	},
	"iso": func(t database.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	},
	"dueDate":  tasks.FormatDate,
	"initials": users.Initials,
	"avatarHue": func(name string) int {
		// A stable colour per person makes names easy to scan in timelines.
		h := 0
		for _, r := range name {
			h = (h*31 + int(r)) % 1200
		}
		return h % 12 // one of twelve avatar colour classes (.hue-0 … .hue-11)
	},
	"priorities":  func() []tasks.Priority { return tasks.Priorities },
	"labelColors": func() []string { return boards.LabelColors },
	"atLeast":     func(r boards.Role, min string) bool { return r.AtLeast(boards.Role(min)) },
	"add":         func(a, b int) int { return a + b },
	"sub":         func(a, b int) int { return a - b },
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	},
	"pct": func(part, total int) int {
		if total == 0 {
			return 0
		}
		return part * 100 / total
	},
	"plural": func(n int, word string) string {
		if n == 1 {
			return "1 " + word
		}
		return fmt.Sprintf("%d %ss", n, word)
	},
	// dict builds a map so partials can receive several values.
	"dict": func(kv ...any) (map[string]any, error) {
		if len(kv)%2 != 0 {
			return nil, fmt.Errorf("dict: odd number of arguments")
		}
		m := make(map[string]any, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			k, ok := kv[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
			}
			m[k] = kv[i+1]
		}
		return m, nil
	},
	"activityIcon": func(action string) string {
		switch action {
		case "task_created":
			return "＋"
		case "comment_added", "comment_edited", "comment_deleted":
			return "💬"
		case "completed":
			return "✓"
		case "reopened":
			return "↺"
		case "moved", "reordered":
			return "→"
		case "assigned", "reassigned", "unassigned":
			return "👤"
		case "priority_changed":
			return "!"
		case "due_date_changed":
			return "📅"
		case "task_deleted":
			return "✕"
		case "labels_changed":
			return "#"
		}
		return "✎"
	},
}
