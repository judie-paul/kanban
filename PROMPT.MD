# Project: Multi-User Kanban Task Management Application

Act as a senior Go backend engineer and full-stack web developer. Build a complete, functional Kanban task management web application.

The application should be clean, maintainable, secure, easy to use, and built with simplicity in mind. Avoid unnecessary abstractions, dependencies, and technologies.

## 1. Technology Stack and Constraints

Use the following technologies:

- **Backend:** Go (Golang).
- **HTTP server:** Go standard library `net/http`.
- **Routing:** Go 1.22+ enhanced `ServeMux` routing patterns. Avoid third-party routers unless there is a compelling reason.
- **Database:** SQLite.
- **Database access:** Use `database/sql` with an appropriate SQLite driver.
- **Frontend:** HTML templates using Go's `html/template` package.
- **Styling:** Plain CSS.
- **JavaScript:** Vanilla JavaScript only, where necessary.
- **Logging:** Go's standard library `log/slog`.
- **Testing:** Go's standard testing package.

### Important restrictions

1. Do NOT use React, Vue, Angular, Svelte, Next.js, or any other frontend framework.
2. Do NOT use Node.js or npm as part of the application runtime.
3. Do NOT introduce a frontend build system.
4. Do NOT use an ORM. Use SQL queries through `database/sql`.
5. Do NOT introduce microservices. Build a single Go application.
6. Prefer the Go standard library whenever practical.
7. Keep dependencies to a minimum.
8. Render pages on the server using Go HTML templates.

The application should be deployable as a single Go application with an SQLite database.

## 2. Application Overview

Build a multi-user Kanban task management application.

Multiple registered users should be able to log in and collaborate on shared boards.

The application should allow users to:

- Create and manage boards.
- Organize tasks into columns.
- Create, edit, delete, and move tasks.
- Assign tasks to users.
- Track who created each task.
- Track who performed every significant action on a task.
- View the complete history of a task.

The most important requirement is accountability and transparency.

**Every task must clearly show who posted it, and every subsequent action must be attributed to the user who performed it.**

For example, if Alice creates a task and Bob later changes its priority, the application must preserve that information.

## 3. User Authentication and Management

Implement user authentication.

Features:

- User registration.
- User login.
- User logout.
- Password hashing using a secure password-hashing algorithm such as bcrypt or Argon2id.
- Session-based authentication using secure cookies.
- Protected routes that require authentication.
- User profile displaying their name and email.

Users should have:

- Unique ID.
- Full name.
- Email address.
- Password hash.
- Created timestamp.

Do not store plaintext passwords.

A user must only access boards they own or have been granted access to.

## 4. Boards

A board represents a project or a collection of related tasks.

Users should be able to:

- Create boards.
- View boards they own or belong to.
- Edit board names and descriptions.
- Delete boards they own.
- Invite or add other registered users to boards.
- Remove members from boards.

Each board should have an owner.

Implement board membership with roles:

- Owner: Full control over the board, including deleting it and managing members.
- Admin: Can manage board settings, columns, and tasks.
- Member: Can create, edit, move, and comment on tasks.

Enforce these permissions on the backend, not just in the frontend.

## 5. Kanban Columns

Every board should have default columns:

1. Backlog
2. To Do
3. In Progress
4. Review
5. Done

Users with appropriate permissions should be able to:

- Create columns.
- Rename columns.
- Delete columns.
- Reorder columns.

Each column should belong to a board and have a position.

Do not allow deletion of a column containing tasks without first providing a way to move those tasks to another column.

## 6. Tasks

Tasks are the central feature of the application.

Each task should contain:

- ID.
- Board ID.
- Column ID.
- Title.
- Description.
- Priority (Low, Medium, High, Urgent).
- Due date (optional).
- Assignee (optional).
- Position within its column.
- Creator ID.
- Created timestamp.
- Last updated timestamp.

The application should allow authorized users to:

- Create tasks.
- Edit tasks.
- Delete tasks.
- View task details.
- Move tasks between columns.
- Reorder tasks within columns.
- Assign tasks to board members.
- Change task priority.
- Set or change due dates.
- Add labels.
- Add comments.

### Important: Task ownership and accountability

Every task must display its creator prominently.

For example:

Task: Implement user authentication

Posted by: Alice Johnson
Created: November 4, 2026
Assigned to: Bob Smith
Priority: High
Status: In Progress

The person who created a task and the person currently assigned to it are different concepts. Preserve this distinction.

Never overwrite the creator when a task is edited or reassigned.

## 7. Task Activity and Audit History

This is one of the most important features of the application.

Implement a task activity log that records significant actions performed on every task.

Every activity entry must record:

- Activity ID.
- Task ID.
- User ID of the person performing the action.
- Action type.
- Description of the action.
- Timestamp.
- Previous value, where applicable.
- New value, where applicable.

Record activities such as:

- Task created.
- Task title changed.
- Task description changed.
- Priority changed.
- Due date changed.
- Task assigned to a user.
- Task reassigned.
- Task moved to another column.
- Task reordered.
- Comment added.
- Task completed.
- Task reopened.

### Example activity history

For a task called "Implement user authentication":

Alice Johnson created this task.
November 4, 2026, 9:00 AM

Bob Smith was assigned to this task by Alice Johnson.
November 4, 2026, 9:15 AM

Bob Smith moved this task from To Do to In Progress.
November 4, 2026, 10:30 AM

Bob Smith changed priority from Medium to High.
November 4, 2026, 11:00 AM

Carol Williams commented: "Authentication is now implemented."
November 4, 2026, 2:00 PM

### Important audit requirements

1. Activity records must be generated on the backend.
2. Never trust the frontend to tell the server who performed an action.
3. Derive the acting user from the authenticated session.
4. Activity history must be persistent in SQLite.
5. Do not allow ordinary users to edit or delete activity history.
6. Display the user's name and action in a readable activity timeline.
7. Record task modifications and their corresponding activity entries within the same database transaction wherever applicable.

When displaying a task, users should be able to open its details and see its complete activity history.

The interface should make it immediately clear who created the task, who is assigned to it, and who performed subsequent actions.

## 8. Comments

Users should be able to comment on tasks.

Each comment should have:

- ID.
- Task ID.
- Author ID.
- Comment content.
- Created timestamp.
- Updated timestamp.

Display the author's name and timestamp alongside each comment.

Users may edit or delete their own comments. Board owners and admins may moderate comments according to board permissions.

Record comment creation and other significant comment actions in the activity history.

## 9. Search, Filtering and Sorting

Implement:

- Search tasks by title.
- Filter tasks by status/column.
- Filter by priority.
- Filter by assignee.
- Filter by creator.
- Filter by due date.
- Sort by creation date.
- Sort by due date.
- Sort by priority.

Keep the interface straightforward and responsive.

## 10. Dashboard

Create a dashboard that displays the boards accessible to the logged-in user.

The dashboard should include:

- A list of boards.
- Board names and descriptions.
- Number of tasks per board.
- Number of completed tasks.
- Number of overdue tasks.
- Recently updated tasks.
- Tasks assigned to the logged-in user.

Users should be able to create a board directly from the dashboard.

### Kanban board interface

The main board page should display columns horizontally.

Each task should appear as a card showing:

- Task title.
- Priority.
- Due date, if present.
- Assignee.
- Creator (posted by).
- Relevant labels.

Clicking a card should open a task details view, preferably as a modal or side panel.

The task details view should contain:

- Full task description.
- Creator information.
- Assignee information.
- Priority and due date.
- Comments.
- Complete activity history.

Provide drag-and-drop task movement using vanilla JavaScript. The backend must validate and persist every move.

The application must also support task movement through a normal form or accessible alternative so that drag-and-drop is not the only way to move tasks.

## 11. Notifications

Implement basic in-app notifications.

Notify users when:

- They are assigned a task.
- Someone comments on a task assigned to them.
- They are mentioned in a comment, if mentions are implemented.
- A task they created is updated or completed.

Each notification should include:

- Recipient.
- Actor.
- Message.
- Related task.
- Created timestamp.
- Read/unread status.

Allow users to mark notifications as read.

Real-time notifications and WebSockets are not required for the initial version. Polling or refreshing the dashboard is sufficient.

## 12. Database Design

Use SQLite and create a sensible relational database schema.

At minimum, consider these tables:

- users
- sessions
- boards
- board_members
- columns
- tasks
- labels
- task_labels
- comments
- task_activities
- notifications

Requirements:

- Use foreign key constraints.
- Add appropriate indexes.
- Use transactions for operations that modify related records.
- Use prepared statements and parameterized queries.
- Enforce unique email addresses.
- Store timestamps consistently, preferably UTC.
- Enable SQLite foreign key enforcement.
- Configure SQLite connection pooling appropriately.
- Include database migrations or a reliable schema initialization mechanism.

Avoid unnecessary tables and overengineering.

## 13. Go Project Architecture

Organize the application into clear, maintainable packages.

Use a structure similar to:

```text
kanban/
├── cmd/
│   └── web/
│       └── main.go
│
├── internal/
│   ├── auth/
│   ├── users/
│   ├── boards/
│   ├── tasks/
│   ├── comments/
│   ├── activity/
│   ├── notifications/
│   ├── database/
│   ├── middleware/
│   └── web/
│
├── templates/
│   ├── layouts/
│   ├── pages/
│   └── partials/
│
├── static/
│   ├── css/
│   ├── js/
│   └── images/
│
├── migrations/
│
├── go.mod
└── README.md
```

You may adjust this structure if you have a good architectural reason, but keep the application easy to understand.

### Architectural principles

- Keep HTTP handlers focused on HTTP concerns.
- Put business logic in appropriate service or domain components.
- Keep SQL queries and database operations organized.
- Avoid massive handler functions.
- Avoid unnecessary interfaces and abstractions.
- Pass dependencies explicitly.
- Use context appropriately.
- Handle errors consistently.
- Use middleware for authentication, logging, and authorization.
- Use `slog` for structured logging.

Do not create abstractions simply for the sake of creating abstractions.

## 14. Security

Implement sensible security practices:

- Password hashing.
- Secure session management.
- HTTP-only session cookies.
- SameSite cookie protection.
- CSRF protection for state-changing requests.
- Input validation.
- HTML escaping through `html/template`.
- SQL injection prevention.
- Authorization checks on every protected resource.
- Prevent users from accessing boards they do not belong to.
- Do not expose internal errors or sensitive information to users.

## 15. Testing

Write meaningful tests.

At minimum, test:

### Authentication
- User registration.
- Duplicate email rejection.
- Login with valid credentials.
- Login with invalid credentials.
- Logout.
- Accessing protected routes without authentication.

### Boards
- Creating a board.
- Adding members.
- Removing members.
- Enforcing board permissions.
- Preventing unauthorized access.

### Tasks
- Creating tasks.
- Editing tasks.
- Assigning tasks.
- Moving tasks between columns.
- Changing priorities.
- Validating task ownership and permissions.

### Activity history
- Recording task creation.
- Recording task updates.
- Recording task movement.
- Correctly identifying the acting user.
- Ensuring activity records persist.
- Ensuring failed operations do not leave incomplete activity records.

Use a temporary SQLite database for tests.

## 16. User Interface and Design

Build a modern, clean, professional interface.

Design principles:

- Minimal and uncluttered.
- Easy to navigate.
- Responsive on desktop, tablet, and mobile.
- Consistent spacing and typography.
- Clear visual distinction between priorities.
- Clear task ownership and assignment information.
- Readable activity timelines.
- Useful empty states.
- Confirmation before destructive operations.
- Accessible forms and controls.

Avoid unnecessary animations and visual complexity.

The application should feel like a polished productivity tool rather than a basic CRUD demonstration.

## 17. Development and Delivery Instructions

Do not attempt to implement the entire application in one enormous code dump.

Work incrementally and systematically.

Follow these steps:

**Step 1: Project planning**
- Review the requirements.
- Propose the architecture.
- Define the database schema.
- Identify the main routes and application workflows.
- Explain important design decisions.

**Step 2: Project foundation**
- Initialize the Go module.
- Set up SQLite.
- Create the database schema and migrations.
- Set up the HTTP server.
- Configure structured logging.
- Set up template rendering and static file serving.

**Step 3: Authentication**
- Implement registration, login, logout, and sessions.
- Write tests.

**Step 4: Boards and membership**
- Implement board management.
- Implement board membership and permissions.
- Write tests.

**Step 5: Tasks and columns**
- Implement Kanban columns and task management.
- Implement task movement and ordering.
- Write tests.

**Step 6: Accountability and collaboration**
- Implement task activity history.
- Implement comments.
- Implement notifications.
- Write tests.

**Step 7: Frontend**
- Build the dashboard.
- Build the Kanban board interface.
- Build task detail views.
- Implement drag-and-drop with vanilla JavaScript.
- Make the application responsive.

**Step 8: Finalization**
- Review security.
- Run all tests.
- Fix bugs.
- Provide setup instructions.
- Document the architecture and important implementation decisions.

### How to work

- First, present the proposed architecture, database schema, routes, and implementation plan.
- Then proceed with implementation in manageable stages.
- Do not skip tests.
- Do not leave placeholder functions or nonfunctional buttons.
- Do not claim a feature is complete unless it actually works.
- Make reasonable decisions independently instead of asking me about every minor implementation detail.
- If a requirement is ambiguous, explain your assumption.
- Keep the project runnable at every major stage.
- Explain significant architectural decisions as you work.

## 18. Definition of Done

The project is complete when:

1. Multiple users can register and log in.
2. Users can create boards and collaborate on shared boards.
3. Users can create and manage Kanban columns.
4. Users can create, edit, assign, and move tasks.
5. Every task displays who originally posted it.
6. Every significant task action records who performed it and what they did.
7. The activity timeline displays a complete, persistent history.
8. Users can comment on tasks.
9. Board membership and permissions are enforced.
10. The dashboard displays useful task and board information.
11. The application uses Go, SQLite, Go HTML templates, CSS, and vanilla JavaScript.
12. Tests pass and the README explains how to run the application.

**Final priority:** Build a reliable, maintainable, multi-user Kanban application where task ownership and activity accountability are first-class features. Favor simplicity, correctness, and good Go engineering practices over unnecessary complexity.