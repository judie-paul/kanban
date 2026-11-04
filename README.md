# Kanban — Team Task Board with Full Accountability

Kanban is a web app where a team can plan and track work on shared boards. Work is split into **tasks** (cards) that move through **columns** such as *To Do → In Progress → Done*.

What sets it apart is **accountability**. Every task shows **who posted it** and **who it is assigned to**. Every change made to a task (editing, moving, reassigning, commenting) is saved in a **permanent history** with the person's name and the time. Nobody has to guess who changed what.

---

## Contents

**Part 1 — For everyone (no technical knowledge needed)**

1. [What you can do with it](#1-what-you-can-do-with-it)
2. [Starting the app](#2-starting-the-app)
3. [Step-by-step guide](#3-step-by-step-guide)
4. [Who can do what (roles)](#4-who-can-do-what-roles)
5. [Frequently asked questions](#5-frequently-asked-questions)

**Part 2 — Technical documentation**

6. [Requirements](#6-requirements)
7. [Building from source](#7-building-from-source)
8. [Running and configuration](#8-running-and-configuration)
9. [Running the tests](#9-running-the-tests)
10. [Project structure](#10-project-structure)
11. [Architecture](#11-architecture)
12. [Database schema](#12-database-schema)
13. [Routes](#13-routes)
14. [Security](#14-security)
15. [Design decisions and assumptions](#15-design-decisions-and-assumptions)
16. [Deploying to a server](#16-deploying-to-a-server)

---

# Part 1 — For everyone

## 1. What you can do with it

- **Create boards** for projects, such as "Website relaunch" or "Hiring".
- **Invite teammates** to a board and give them a role (Admin or Member).
- **Add tasks** with a title, description, priority (Low, Medium, High, Urgent), due date, labels and an assignee.
- **Move tasks** between columns by dragging them, or with the "Move" buttons.
- **See who posted each task.** This never changes, even when someone else edits or reassigns the task.
- **See the complete history** of every task, for example:
  > **Alice Johnson** created this task in To Do — *Nov 4, 2026, 9:00 AM*
  > **Alice Johnson** assigned this task to Bob Smith — *9:15 AM*
  > **Bob Smith** moved this task from To Do to In Progress — *10:30 AM*
  > **Bob Smith** changed priority from Medium to High — *11:00 AM*
  > **Carol Williams** commented: "Authentication is now implemented." — *2:00 PM*
- **Comment** on tasks to discuss the work.
- **Get notified** when you are assigned a task, when someone comments on your task, or when a task you posted is changed or completed.
- **Search, filter and sort** tasks by title, column, priority, assignee, who posted them, label or due date.
- **Use the dashboard** to see all your boards, overdue work, tasks assigned to you and recent changes.

## 2. Starting the app

The app is a single program file with nothing to install. Your data is saved in one file called `kanban.db`, created next to wherever you start the program.

**Windows**

1. Open the `build` folder.
2. Double-click **`kanban-windows-amd64.exe`**. A black window appears. Keep it open while you use the app, because closing it stops the app.
   - If Windows shows "Windows protected your PC", click **More info → Run anyway**. This warning appears because the program isn't signed by a publisher.
3. Open your web browser and go to **http://localhost:8080**

**macOS**

1. Open the **Terminal** app and go to the project's `build` folder.
2. Run `./kanban-darwin-arm64`. Use `./kanban-darwin-amd64` instead on older Intel Macs.
   - If macOS refuses to open it, run `xattr -d com.apple.quarantine kanban-darwin-arm64` once, then try again.
3. Open **http://localhost:8080** in your browser.

**Linux**

1. In a terminal, go to the `build` folder.
2. Run `./kanban-linux-amd64`. If you get "permission denied", run `chmod +x kanban-linux-amd64` first.
3. Open **http://localhost:8080**.

**Using it with your team:** other people on the same network can open `http://<your-computer's-IP-address>:8080`. To put it on the internet, see [Deploying to a server](#16-deploying-to-a-server).

**To stop the app**, close the window or press **Ctrl + C** in it. Your data stays in `kanban.db`, and everything is still there the next time you start the app.

## 3. Step-by-step guide

### Create your account
1. Click **Sign up**.
2. Enter your full name, email address and a password of at least 8 characters.
3. You are logged in and taken to your **Dashboard**.

Every teammate creates their own account the same way. A teammate must have an account before you can add them to a board.

### Create a board
1. On the Dashboard, fill in **Create a board** with a name and an optional description.
2. Click **Create board**. The board comes with five columns: **Backlog, To Do, In Progress, Review, Done**.

### Invite teammates
1. Open the board and click **Settings & members**.
2. Under **Add a member**, enter your teammate's email address and choose **Member** or **Admin**.
3. Click **Add member**. They will see the board on their dashboard and get a notification.

### Add a task
1. At the bottom of any column, click **+ Add a task**.
2. Type a title. You can also pick a priority, an assignee and a due date.
3. Click **Add task**. The task opens so you can add more details.

### Open a task
Click any card. A panel opens on the right showing:
- **Posted by:** the person who created the task, with the date
- **Assigned to:** who is responsible for it now
- **Priority, status (column), due date, labels**
- **Description, comments** and the **full history**

Close the panel with the **×** button, by clicking outside it, or by pressing **Esc**.

### Edit a task
In the task panel, click **✎ Edit task**. You can change the title, description, priority, due date, assignee and labels, then click **Save changes**. Each change is recorded in the history under your name.

### Move a task
- **Drag and drop:** drag a card into another column, or up and down within a column.
- **Without dragging** (keyboard, touch screens and screen readers): open the task and use the **Move** section to choose a column and "top" or "bottom", or click **↑ Move up** / **↓ Move down**.

When a task is moved into **Done**, it is marked **completed**. If it is moved out again, it is marked **reopened**. Both events appear in the history.

### Comment
Open a task, type in **Add a comment** and click **Comment**. You can edit or delete your own comments. The task's assignee and the person who posted it are notified.

### Find tasks
Click **Search, filter & sort** above the columns. You can search titles, filter by column, priority, assignee, who posted the task, label or due date ("Overdue", "Due today", "Due within 7 days", "No due date"), and sort by newest, due date or priority. Click **Clear** to show everything again.

> While a filter or sort is active, drag-and-drop is turned off so cards don't land in unexpected places. The Move buttons inside a task still work.

### Notifications
The **Notifications** link at the top shows a number when you have unread notifications. Click **Open** on a notification to go to that task, or **Mark all as read**. New notifications appear when you reload the page.

### Board activity log
On a board, click **Activity log** to see everything that has happened on it, newest first. This includes tasks that were later deleted.

### Manage columns and labels
In **Settings**, owners and admins can rename, add, reorder (**← Left / Right →**) and delete columns. They can also choose which column counts as "done".

You can't delete a column that still has tasks until you choose another column to move them to. Every member can create labels such as "bug" or "design" and pick a colour.

### Your profile
Click your name at the top right to see your profile, change your display name or change your password. Changing your password signs you out on all your other devices.

## 4. Who can do what (roles)

| Action | Member | Admin | Owner |
|---|:-:|:-:|:-:|
| See the board, its tasks, comments and history | ✅ | ✅ | ✅ |
| Create, edit, move and assign tasks | ✅ | ✅ | ✅ |
| Comment; edit and delete **own** comments | ✅ | ✅ | ✅ |
| Create labels | ✅ | ✅ | ✅ |
| Delete tasks **they posted** | ✅ | ✅ | ✅ |
| Delete **any** task | — | ✅ | ✅ |
| Delete **anyone's** comment (moderation) | — | ✅ | ✅ |
| Edit board name and description; manage columns; delete labels | — | ✅ | ✅ |
| Add and remove members; change roles | — | — | ✅ |
| Delete the board | — | — | ✅ |
| Leave the board | ✅ | ✅ | — |

The server checks these rules on every request, so they can't be bypassed from the browser. People who aren't members of a board can't see it at all.

## 5. Frequently asked questions

**Can the history be edited or deleted?**
No. History entries can only be added. The app has no way to edit or delete them, and the database itself rejects any attempt to change them. History is only removed when the whole board is deleted.

**What happens when a task is deleted?**
The task disappears from the board, but the board's **Activity log** keeps a record saying who deleted it and when.

**What happens when someone is removed from a board?**
They lose access immediately. Any tasks assigned to them become unassigned, and that is recorded in each task's history.

**Who "posted" a task if someone else edits it later?**
Always the original creator. Editing or reassigning a task never changes who posted it.

**Why are times shown in my local time?**
The server stores all times in UTC, and your browser converts them to your time zone. If JavaScript is turned off, times are shown in UTC.

**Where is my data, and how do I back it up?**
Everything is in the `kanban.db` file (plus temporary `kanban.db-wal` and `kanban.db-shm` files while the app runs). To back up, stop the app and copy `kanban.db`.

**I forgot my password.**
The app doesn't send emails, so there is no "forgot password" link. Ask the person who runs the app to set a new password for you (see [Running and configuration](#8-running-and-configuration)), then change it yourself on your profile page.

---

# Part 2 — Technical documentation

## 6. Requirements

| To… | You need |
|---|---|
| **Run** a prebuilt binary | Nothing else. Each binary is self-contained, with templates, CSS, JS and migrations embedded. |
| **Build** from source | [Go 1.26+](https://go.dev/dl/) on Windows, Linux or macOS. Older Go (1.21+) automatically downloads the right toolchain. **No C compiler** is needed, because the SQLite driver ([`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)) is pure Go. |

Stack: Go standard library `net/http` with Go 1.22+ `ServeMux` patterns, `html/template`, `database/sql`, `log/slog`, SQLite, plain CSS and vanilla JavaScript. It has no frontend framework, no Node.js or npm, no build step for the frontend and no ORM.

Direct dependencies:
- `modernc.org/sqlite`: the SQLite driver
- `golang.org/x/crypto/bcrypt`: password hashing

## 7. Building from source

The repository has two top-level folders:

```text
.
├── build/       compiled binaries (output of the build scripts)
├── code/        all source code (Go module "kanban")
├── build.sh     build script for Linux / macOS / Git Bash
├── build.ps1    build script for Windows PowerShell
├── build.bat    wrapper so Windows users can run "build" from cmd.exe
└── README.md
```

**Linux / macOS**

```sh
./build.sh          # build for this machine     -> build/kanban-<os>-<arch>
./build.sh all      # cross-compile for windows/linux/darwin × amd64/arm64
./build.sh test     # go vet + go test
```

**Windows (PowerShell or cmd)**

```powershell
.\build.ps1         # or: build
.\build.ps1 all
.\build.ps1 test
```

**Plain Go commands** work too:

```sh
cd code
go build -o ../build/kanban ./cmd/web      # add .exe on Windows
go run ./cmd/web                           # run without building a binary
```

Builds use `CGO_ENABLED=0`, so any OS can cross-compile for any other. The version string comes from `git describe` and is shown by `kanban -version`.

The repository includes prebuilt binaries for Windows (amd64), Linux (amd64) and macOS (Apple Silicon, arm64). Run `build.sh all` / `build.ps1 all` to produce the others: windows/arm64, linux/arm64 and darwin/amd64.

## 8. Running and configuration

```text
kanban [flags]

  -addr string          HTTP listen address               (default ":8080",     env KANBAN_ADDR)
  -db string            SQLite database file path         (default "kanban.db", env KANBAN_DB)
  -secure-cookies       mark cookies Secure (use with HTTPS)  (default false,  env KANBAN_SECURE_COOKIES)
  -log-format string    "text" or "json"                  (default "text",      env KANBAN_LOG_FORMAT)
  -version              print version and exit
  -reset-password email set a new password for a user (read from stdin) and exit
```

Examples:

```sh
./build/kanban-linux-amd64 -addr :3000 -db /var/lib/kanban/kanban.db
KANBAN_LOG_FORMAT=json ./build/kanban-linux-amd64
```

On startup the app:
1. opens (or creates) the database
2. applies any pending migrations
3. starts the HTTP server

It shuts down gracefully on Ctrl+C or SIGTERM. Expired sessions are cleaned up every hour. A health check is available at `GET /healthz`.

**Resetting a forgotten password** (operator task, run on the machine that holds the database):

```sh
./build/kanban-linux-amd64 -db kanban.db -reset-password bob@example.com
New password for bob@example.com: ********     # type it and press Enter
```

The user is signed out on all devices and can log in with the new password. On Windows, use `build\kanban-windows-amd64.exe` with the same flags.

## 9. Running the tests

```sh
cd code
go test ./...          # or ./build.sh test  /  .\build.ps1 test
go test -v ./internal/tasks/ -run Move   # a single test
```

Each test gets its own temporary, fully migrated SQLite database, which is deleted automatically.

| Package | What is tested |
|---|---|
| `internal/auth` | registration, bcrypt hashing, validation, duplicate email rejection, valid and invalid login, session creation, logout, expiry, password change revoking other sessions, operator password reset |
| `internal/boards` | default columns, owner membership, add and remove members, role permissions (member, admin, owner), non-member access denied, column reordering, leaving a board |
| `internal/tasks` | creation with creator and activity, validation, per-field edit history, creator never overwritten, assign/reassign/unassign, moving between columns, completion and reopening, reordering, permissions, deleted-task audit trail, append-only history, **failed operations leaving no activity or notifications**, column deletion moving tasks, filters and sorting, labels, board deletion cascade |
| `internal/comments` | comment activity, notifications without self-notification, author-only edit, owner moderation, notification read permissions |
| `internal/web` | HTTP-level: protected routes redirect to login, CSRF enforcement, register/login/logout flow, open-redirect protection, an end-to-end flow (board, member, task, drag-and-drop JSON move, edit, comment) checking the rendered history, every page rendering, static files, security headers |
| `internal/database` | idempotent migrations, foreign keys enabled, timestamp scanning |

## 10. Project structure

```text
code/
├── assets.go                  embeds templates/, static/, migrations/ into the binary
├── cmd/web/main.go            entry point: flags, logging, DB, migrations, HTTP server
├── internal/
│   ├── apperr/                shared error kinds: NotFound, Forbidden, Validation
│   ├── database/              Open (pragmas, pool), Migrate, WithTx, timestamp helpers
│   ├── users/                 user accounts
│   ├── auth/                  registration, login, bcrypt, sessions
│   ├── boards/                boards, membership and roles, columns, labels
│   ├── tasks/                 tasks: CRUD, assignment, move/reorder, filters, column deletion
│   ├── comments/              task comments
│   ├── activity/              append-only audit log (Record + queries)
│   ├── notifications/         in-app notifications
│   ├── middleware/            logging, recovery, security headers, CSRF, auth, body limit
│   ├── web/                   routes, handlers, template rendering
│   └── testdb/                temporary migrated database for tests
├── migrations/0001_initial_schema.sql
├── templates/
│   ├── layouts/base.html
│   ├── pages/                 dashboard, board, settings, activity, notifications, profile, auth, error
│   └── partials/              components (cards, badges, timeline), task_panel
└── static/
    ├── css/app.css            all styles (light and dark theme)
    ├── js/app.js              drag-and-drop and small enhancements
    └── images/logo.svg
```

## 11. Architecture

```text
Browser ──HTTP──▶ middleware chain ──▶ ServeMux route ──▶ handler (internal/web)
                  Recover                                    │ parses form/query
                  SecureHeaders                              │ takes user from session context
                  Authenticate (session → user)              ▼
                  Logging (slog)                       domain service (boards / tasks / comments …)
                  LimitBody                                  │ permission check (board role)
                  CSRF                                       │ validation
                                                             │ BEGIN
                                                             │   change rows
                                                             │   activity.Record(...)
                                                             │   notifications.Create(...)
                                                             │ COMMIT
                                                             ▼
                                                          SQLite
```

- **Handlers only deal with HTTP.** They read the request and take the acting user from the session (`middleware.UserFrom`). They call a service, then render a template or redirect with a flash message. `handleError` turns service errors into responses: validation errors become a flash message plus redirect (or a JSON error), `ErrForbidden` becomes a flash message, `ErrNotFound` becomes a 404 page, and anything else becomes a generic 500 that is logged.
- **Services own the business rules and authorization.** Every method that acts for a user takes the actor's ID and checks their board role first (`boards.Require`). Permissions are therefore enforced in one place, and the service tests can check them without HTTP.
- **Changes and their audit records are written in one transaction.** `database.WithTx` wraps every multi-step write. A task change, its activity entries and its notifications either all commit or all roll back. A test forces a failure after the activity rows have been written and checks that nothing remains.
- **The acting user always comes from the session.** Activity and notification records take the user ID from the authenticated session, never from form fields. A test posts a forged `creator_id` and checks that it is ignored.
- **Few abstractions.** There are plain structs with a `*sql.DB`, SQL in the package that owns the table, and one small interface (`database.DBTX`) so helpers like `activity.Record` can run inside or outside a transaction.
- **Progressive enhancement.** Every feature works with plain links and forms. The task panel is rendered by the server (`/boards/{id}?task={tid}`). JavaScript adds drag-and-drop through `POST /tasks/{id}/move` with `Accept: application/json`, local times, confirmations and Esc-to-close.

## 12. Database schema

SQLite configuration (`internal/database`): `foreign_keys=ON`, WAL journal, `busy_timeout=5000`, `synchronous=NORMAL`, and `BEGIN IMMEDIATE` transactions, which avoid lock-upgrade deadlocks. The pool has 8 connections. Timestamps are UTC text in the fixed-width format `YYYY-MM-DDTHH:MM:SSZ`, so they sort correctly as strings. Due dates are `YYYY-MM-DD`.

| Table | Purpose / key columns |
|---|---|
| `users` | `id, name, email UNIQUE COLLATE NOCASE, password_hash, created_at` |
| `sessions` | `token_hash` (SHA-256 of the cookie token) `PK, user_id, created_at, expires_at` |
| `boards` | `id, name, description, owner_id, created_at, updated_at` |
| `board_members` | `(board_id, user_id) PK, role CHECK IN (owner, admin, member), added_at` |
| `columns` | `id, board_id, name, position, is_done` (tasks in done columns count as completed) |
| `tasks` | `id, board_id, column_id, title, description, priority CHECK, due_date, assignee_id NULL, position, creator_id NOT NULL, created_at, updated_at, completed_at` |
| `labels` / `task_labels` | board-scoped coloured labels and the many-to-many link |
| `comments` | `id, task_id, author_id, content, created_at, updated_at` |
| `task_activities` | `id, task_id NULL (ON DELETE SET NULL), board_id, task_title snapshot, user_id, action, description, old_value, new_value, created_at` |
| `notifications` | `id, recipient_id, actor_id, task_id NULL, board_id NULL, message, is_read, created_at` |
| `schema_migrations` | applied migration versions |

Indexes cover the common lookups: membership by user, tasks by column and position, tasks by board, assignee, creator and `updated_at`, activity by task and by board, notifications by recipient and read state.

The **append-only trigger** `task_activities_append_only` aborts any `UPDATE` of an activity row's content. Only the foreign key action that sets `task_id` to NULL after a task is deleted is allowed through.

Migrations are numbered `.sql` files in `code/migrations/`, embedded in the binary. Each runs once, in its own transaction. To change the schema, add `0002_<name>.sql`.

## 13. Routes

All routes except the public ones require login. All `POST` requests require a CSRF token.

| Method & path | Purpose |
|---|---|
| `GET /` | landing page (redirects to the dashboard when logged in) |
| `GET/POST /register`, `GET/POST /login`, `POST /logout` | authentication |
| `GET /profile`, `POST /profile`, `POST /profile/password` | profile, change name or password |
| `GET /dashboard` | boards, stats, assigned tasks, recent updates |
| `POST /boards` | create a board |
| `GET /boards/{id}` | Kanban view. Query parameters: `q, column, priority, assignee (-1 = unassigned), creator, label, due (overdue/today/week/none), sort (created/due/priority), task` (opens the panel) |
| `GET /boards/{id}/settings`, `GET /boards/{id}/activity` | settings; board activity log |
| `POST /boards/{id}/update` · `/delete` · `/leave` | board details, delete (owner, requires typing the name), leave |
| `POST /boards/{id}/members` · `/members/{uid}/role` · `/members/{uid}/remove` | membership (owner) |
| `POST /boards/{id}/columns` · `/columns/{cid}/update` · `/reorder` · `/delete` | columns (admin+) |
| `POST /boards/{id}/labels` · `/labels/{lid}/delete` | labels |
| `POST /boards/{id}/tasks` | create a task |
| `GET /tasks/{id}` | permalink that redirects to the board with the panel open |
| `POST /tasks/{id}/update` · `/move` · `/delete` | edit; move (form or JSON); delete |
| `POST /tasks/{id}/comments`, `POST /comments/{id}/update` · `/delete` | comments |
| `GET /notifications`, `POST /notifications/{id}/read`, `POST /notifications/read-all` | notifications |
| `GET /static/…`, `GET /healthz` | assets, health check |

## 14. Security

- **Passwords** are hashed with bcrypt (default cost). Unknown emails are still checked against a dummy hash, so the response time doesn't reveal which accounts exist. The error message is the same for a wrong email and a wrong password.
- **Sessions** use a random 256-bit token in an `HttpOnly`, `SameSite=Lax` cookie (`Secure` when `-secure-cookies` is set). Only the token's SHA-256 hash is stored. Sessions expire after 7 days. Login replaces any existing session, which prevents session fixation. Changing the password revokes the user's other sessions.
- **CSRF protection** uses the double-submit pattern. A random token is kept in an HttpOnly cookie and must be sent back as the `csrf_token` form field or the `X-CSRF-Token` header on every non-GET request. `SameSite` cookies add a second layer.
- **Authorization** is checked in the service layer on every operation. Non-members get *404 Not Found*, so a board's existence isn't revealed. IDs from forms, such as assignee, labels and target column, are checked against the board.
- **Injection:** all SQL uses parameterized queries. `LIKE` searches escape `%` and `_`. Every value in HTML is escaped by `html/template`.
- **Headers:** a strict Content-Security-Policy (`default-src 'self'`, no inline scripts or styles), `X-Frame-Options: DENY`, `nosniff` and `Referrer-Policy`.
- **Other protections:** request bodies are limited to 1 MB, server timeouts are set, redirects only go to local paths (no open redirects), and panics are recovered and logged without leaking details. Users see generic messages for internal errors.

## 15. Design decisions and assumptions

- **Pure-Go SQLite driver.** `modernc.org/sqlite` avoids CGO, so the project builds on Windows and Unix without a C toolchain and cross-compiles easily.
- **Embedded assets.** The single binary contains templates, CSS, JS and migrations. This makes deployment as simple as copying one file.
- **Completion is based on columns.** A column can be flagged as "done" (the default *Done* column is). Moving a task into a done column records **completed**, and moving it out records **reopened**. Completion counts and overdue counts on the dashboard use this flag.
- **Deleting a task keeps its history.** `task_activities.task_id` becomes NULL but the row stays, with a snapshot of the task title. The board activity log therefore still shows who deleted what.
- **Deleting a column** requires choosing a destination column for its tasks. Each task move is recorded with the reason. A board always keeps at least one column.
- **Activity descriptions are stored as text**, for example "changed priority from Medium to High", alongside structured `action`, `old_value` and `new_value` fields. The actor's name is joined in when the history is displayed.
- **Permission choices** where the brief left room for interpretation:
  - Only the **owner** manages members.
  - **Admins** manage board details, columns and labels, and can moderate comments and delete any task.
  - **Members** can create labels, and can delete tasks they posted.
  - Ownership transfer isn't supported.
- **Notifications** are sent for:
  - being added to a board
  - being assigned a task
  - a comment on a task assigned to you
  - a comment on a task you posted
  - any update, move, completion or deletion of a task you posted

  You never get a notification about your own actions. `@mentions` aren't implemented (the brief made them optional). Notifications appear when the page is reloaded; there is no live push.
- **Drag-and-drop is turned off while filters or sorting are active.** Positions are relative to the whole column, so dropping a card into a filtered list would be ambiguous. The Move controls in the task panel always work.
- **Times** are stored in UTC and shown in the viewer's local time by JavaScript. "Overdue" compares due dates against the current UTC date.
- **Not included:** email delivery (invitations, self-service password reset), file attachments, real-time updates. Each of these would need extra infrastructure such as SMTP, file storage or WebSockets, and the brief said they weren't needed. Forgotten passwords are handled by the operator command `-reset-password`.

## 16. Deploying to a server

1. Copy the binary for the server's OS (for example `build/kanban-linux-amd64`) to the server.
2. Run it behind a reverse proxy that handles HTTPS, such as Caddy or nginx, and pass `-secure-cookies`:
   ```sh
   ./kanban-linux-amd64 -addr 127.0.0.1:8080 -db /var/lib/kanban/kanban.db -secure-cookies -log-format json
   ```
3. Keep it running with a systemd unit (or similar), for example:
   ```ini
   [Unit]
   Description=Kanban
   After=network.target

   [Service]
   ExecStart=/opt/kanban/kanban-linux-amd64 -addr 127.0.0.1:8080 -db /var/lib/kanban/kanban.db -secure-cookies -log-format json
   Restart=on-failure
   User=kanban

   [Install]
   WantedBy=multi-user.target
   ```
4. **Back up** `kanban.db` regularly, for example with `sqlite3 kanban.db ".backup backup.db"`, or by copying the file while the service is stopped.
