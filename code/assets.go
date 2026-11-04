// Package kanban holds the files that are compiled into the binary:
// HTML templates, static assets (CSS/JS/images) and SQL migrations.
//
// Embedding them means the application ships as a single executable that
// can be copied anywhere and run without any accompanying folders.
package kanban

import "embed"

// Templates contains templates/layouts, templates/pages and templates/partials.
//
//go:embed templates
var Templates embed.FS

// Static contains static/css, static/js and static/images.
//
//go:embed static
var Static embed.FS

// Migrations contains the numbered *.sql schema migrations.
//
//go:embed migrations/*.sql
var Migrations embed.FS
