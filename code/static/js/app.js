// Kanban — small progressive enhancements in vanilla JavaScript.
// Every feature here also works without JavaScript through normal forms and
// links; this file only makes things smoother (local times, confirmations,
// closing the task panel with Esc, and drag-and-drop).
(function () {
  "use strict";

  const csrfToken = () => document.querySelector('meta[name="csrf-token"]')?.content || "";

  // ---- show timestamps in the viewer's local time zone --------------------
  function localizeTimes(root) {
    const fmt = new Intl.DateTimeFormat(undefined, {
      year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit",
    });
    root.querySelectorAll("time[data-local]").forEach((el) => {
      const d = new Date(el.getAttribute("datetime"));
      if (!isNaN(d)) {
        el.title = el.textContent.trim();
        el.textContent = fmt.format(d);
      }
    });
  }

  // ---- confirmations before destructive actions ---------------------------
  document.addEventListener("submit", (e) => {
    const form = e.target.closest("form[data-confirm]");
    if (form && !window.confirm(form.dataset.confirm)) {
      e.preventDefault();
    }
  });

  // ---- auto-submitting selects (e.g. member roles) ------------------------
  document.addEventListener("change", (e) => {
    if (e.target.matches("select[data-autosubmit]")) {
      e.target.form.requestSubmit();
    }
  });

  // ---- flash messages ------------------------------------------------------
  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-dismiss]");
    if (btn) btn.parentElement.remove();
  });

  // ---- task detail panel ---------------------------------------------------
  function setupPanel() {
    const panel = document.querySelector(".task-panel");
    if (!panel) return;
    const close = () => { window.location.href = panel.dataset.closeUrl; };
    document.body.style.overflow = "hidden";
    const heading = panel.querySelector("#task-title");
    if (heading && !location.hash) {
      heading.tabIndex = -1;
      heading.focus({ preventScroll: true });
    }
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && !e.target.matches("input, textarea, select")) close();
    });
    document.querySelector(".panel-backdrop")?.addEventListener("click", close);
  }

  // ---- drag and drop -------------------------------------------------------
  // The server validates and persists every move; the DOM is only updated
  // once the server has accepted it.
  function setupDragAndDrop() {
    const board = document.querySelector(".board[data-dnd]");
    if (!board) return;

    let dragged = null;
    const placeholder = document.createElement("li");
    placeholder.className = "drop-placeholder";
    placeholder.setAttribute("aria-hidden", "true");

    // Links are draggable by default; let the whole card be the drag handle.
    board.querySelectorAll(".card a").forEach((a) => { a.draggable = false; });

    board.addEventListener("dragstart", (e) => {
      const card = e.target.closest(".card[draggable='true']");
      if (!card) return;
      dragged = card;
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", card.dataset.taskId);
      requestAnimationFrame(() => card.classList.add("dragging"));
    });

    board.addEventListener("dragend", () => {
      if (dragged) dragged.classList.remove("dragging");
      placeholder.remove();
      board.querySelectorAll(".drop-target").forEach((el) => el.classList.remove("drop-target"));
      dragged = null;
    });

    board.addEventListener("dragover", (e) => {
      if (!dragged) return;
      const list = e.target.closest(".column")?.querySelector(".cards");
      if (!list) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      board.querySelectorAll(".drop-target").forEach((el) => el !== list && el.classList.remove("drop-target"));
      list.classList.add("drop-target");
      const after = cardAfter(list, e.clientY);
      if (after) list.insertBefore(placeholder, after);
      else list.appendChild(placeholder);
    });

    board.addEventListener("drop", async (e) => {
      if (!dragged) return;
      const list = e.target.closest(".column")?.querySelector(".cards");
      if (!list) return;
      e.preventDefault();

      // Position = number of real cards before the placeholder.
      let position = 0;
      for (const el of list.children) {
        if (el === placeholder) break;
        if (el.classList.contains("card") && el !== dragged) position++;
      }
      const card = dragged;
      const fromList = card.parentElement;
      const target = placeholder;

      card.classList.add("saving");
      try {
        const body = new URLSearchParams({ column_id: list.dataset.columnId, position: String(position) });
        const res = await fetch(`/tasks/${card.dataset.taskId}/move`, {
          method: "POST",
          headers: {
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
            "X-CSRF-Token": csrfToken(),
          },
          body,
          credentials: "same-origin",
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.ok) throw new Error(data.error || "The task could not be moved.");
        if (target.parentElement === list) list.insertBefore(card, target);
        else list.appendChild(card);
        updateCounts([fromList, list]);
      } catch (err) {
        window.alert(err.message + " The board will reload.");
        window.location.reload();
      } finally {
        card.classList.remove("saving");
        placeholder.remove();
      }
    });

    function cardAfter(list, y) {
      const cards = [...list.querySelectorAll(".card:not(.dragging)")];
      return cards.find((c) => {
        const r = c.getBoundingClientRect();
        return y < r.top + r.height / 2;
      });
    }

    function updateCounts(lists) {
      new Set(lists).forEach((list) => {
        const column = list.closest(".column");
        const n = list.querySelectorAll(".card").length;
        const count = column.querySelector(".count");
        if (count) {
          count.textContent = n;
          count.setAttribute("aria-label", n + " tasks");
        }
        const empty = column.querySelector(".column-empty");
        if (empty) empty.hidden = n > 0;
      });
    }
  }

  document.addEventListener("DOMContentLoaded", () => {
    localizeTimes(document);
    setupPanel();
    setupDragAndDrop();
  });
})();
