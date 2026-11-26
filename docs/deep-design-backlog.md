# Deep design backlog

Open design changes for the Deep UI (`ui/notebook.html`, `ui/toast.css`).

## To do

- [ ] **Decide whether to keep or remove `POST /notebook/{tag}/posts`.**
  - The notebook view no longer calls it (see Done). It is still registered in `server/routes.go` (`createPostHandler` in `server/notebook_handlers.go`) and listed in `README.md`.

- [ ] **Let the menu bar choose between sidebar menus.**
  - The menu bar has one sidebar icon today (notebooks). The Quick note icon switches the main area, not the sidebar. When a second sidebar menu is added, its icon should switch the sidebar's content, and the sidebar close icon and open/closed state should work for any menu. See the menu bar item under Done.

- [ ] **Add infinite scroll to the notebook's post stream.**
  - Today the stream panel loads one page of posts (50 by default, at most 200) and shows a "Load older posts" button (`loadMorePosts()`, `#stream-more` in `ui/notebook.html`). Replace the button with automatic loading: when the bottom of the list scrolls into view, fetch the next page.
  - No server change should be needed. `GET /notebook/{tag}` already pages with `limit` and the `before` cursor (`next_before` from the previous page). See the paging item under Done.
  - To decide: whether to keep a visible end-of-stream line or a fallback button; a loading indicator while a page is in flight; making sure one page is never requested twice at once; and making sure a late page cannot land in a different notebook if the user switches tags (`openTag()` already has a generation guard for the first page).

- [ ] **Show the number of posts above the list of posts.** (added 2026-11-25)
  - Today the count ("Showing X of Y posts") is in the footer under the stream (`#stream-more`, drawn by `updateStreamFooter()` in `ui/notebook.html`), so it is only visible after scrolling to the bottom. Steve wants the number at the top of the stream panel, above the first post.
  - The data is already there: `total` (posts in this view) and `archived_count` come back from `GET /notebook/{tag}`, and the UI keeps them in `streamTotal` and `streamArchived`. No server change should be needed.
  - To decide: whether it replaces the footer count or the footer keeps the "Load older posts" button and "Show N archived" toggle; whether it says "N posts" or keeps "Showing X of Y"; whether the "Show N archived" toggle moves up with it; and what it says when the stream is empty or every post is archived. It should stay in view or at least update when a post is archived or the archived toggle changes (`archivePost()` and `toggleArchived()` already call `updateStreamFooter()`).

## Done

- [x] **Add archiving of posts (hide a post without deleting it).** (2026-11-25; Deep commits `66c1a97` and `b595612` (button styling) on branch `feature/archive-posts`, merged to `develop`; Hatcheck commit `9806e99` on branch `feature/relations-by-predicate`, merged to `develop-go`; both merges were fast-forwards)
  - Posts can't be deleted (Hatcheck is content-addressable and append-only), so archiving is the substitute. Options compared in "Plans for Offering Deletion or Archiving of Deep Posts".
  - Scope is global: an archived post is hidden in every notebook it appears in.
  - Stored as one relation predicate, `archived`, with the state in the relation's `to` field (`"true"` / `"false"`) and `from` = the post's hash. The latest entry for a post wins. One predicate is used because relations carry no timestamp, so entries under two predicates could not be ordered. Re-posting an identical relation still appends a log entry, so archive / unarchive / archive again works.
  - Hatcheck: new `GET /relations?rel=<predicate>` returns every relation with that predicate in log order (`Store.RelationsByPredicate`). With `?hash=` as well, each side is filtered to the predicate.
  - Deep: `GET /notebook/{tag}` drops archived posts before paging, so pages stay full. `total` excludes archived unless shown, and `archived_count` says how many are archived. `?archived=true` includes them, flagged `archived: true`. `POST /posts/{hash}/archive` and `POST /posts/{hash}/unarchive` write the relation.
  - UI: Archive / Unarchive button per post, an "Archived" badge, and a "Show N archived" / "Hide archived" toggle in the stream footer. The toggle reloads only the stream, never the document text.
  - Deep needs Hatcheck built from the new code. Hatcheck's `hatcheck.sh` does not rebuild on `start` or `restart`, so run `./hatcheck.sh build && ./hatcheck.sh restart` after switching branches.
  - Not built: bulk archive (each post is one relation write, so it would need a confirm step). Archive does not check that the hash is a real post.
  - Known: a failed read of the archived set fails the notebook load. The archived set is read in full on every load, and each archive or unarchive adds a log entry, so consider caching if it grows into many thousands. Hatcheck's treemap does not show these relations, because Deep's posts have no name and the treemap starts from named objects. Tested by hand against a real Hatcheck on 2026-11-25.

- [x] **Page the notebook stream so large tags open quickly.** (2026-11-16, commit `c2fbebe` on branch `feature/notebook-paging`, now on `develop`)
  - `GET /notebook/{tag}` used to fetch every post in a tag, two Hatcheck calls each and one after another, so a tag with thousands of posts took minutes to open. It now returns one page, newest first (default 50, at most 200), with `total`, `has_more` and a `next_before` cursor. `?limit=` and `?before=` select a page.
  - Hatcheck returns a tag's hashes in stored order, so Deep pages from the newest end and never fetches the posts it skips. Posts on a page are fetched 8 at a time.
  - The notebook page has a "Load older posts" button that appends a page without touching the document text.
  - Added `server/notebook_paging_test.go`. The infinite scroll item under To do builds on this.

- [x] **Add a Quick note item to the menu bar.** (2026-11-06, commit `aaf02b1` on branch `feature/menu-quick-note`, merged to `develop` the same day)
  - New document-icon button below the notebooks icon. It calls `showHome()`, so it returns to the home view (quick-compose editor) from any open notebook.
  - It shows the pressed state (`aria-pressed`) while the home view is showing; `showHome()` sets it and `openTag()` clears it.
  - Replaces the way back that was lost when "All notebooks" was removed. Updated the Navigation line in `docs/deep-two-panel-design-summary.md`.

- [x] **Remove the "All notebooks" button from the UI.** (2026-11-06, commit `ea3308f` on branch `feature/remove-all-notebooks`, merged to `develop` the same day)
  - Removed the button, and with it the now-empty tag bar (`.tag-bar` markup, CSS, and its show/hide line on login). Removed the `home-btn` lines in the login handler, `showHome()` and `openTag()`. `showHome()` itself stays, since it still runs after login.
  - This left no button back to the home view (quick-compose editor) from an open notebook; the Quick note menu item above restores it.
  - Updated the Navigation line in `docs/deep-two-panel-design-summary.md`.

- [x] **Remove the "Open" tag input from the tag bar.** (2026-11-06, commit `ded54ef`, developed on `feature/remove-open-input`, merged into `feature/menu-bar-sidebar` and then `develop` the same day)
  - Removed the tag text field and its Open button (`.tag-input-wrap`, `.tag-input`), the `openTagFromInput()` handler, and the two lines in `openTag()` and `showHome()` that kept the field in sync. The tag bar now holds only the "All notebooks" button.
  - Notebooks are now opened from the sidebar list. A new notebook is created by posting text with a new `#tag` from the home view's quick-compose editor.
  - No docs changed; `docs/deep-two-panel-design-summary.md` did not mention the input.

- [x] **Add a narrow menu bar that opens and closes the sidebar.** (2026-11-05, commit `1fad87c` on branch `feature/menu-bar-sidebar`, merged to `develop` on 2026-11-06)
  - Layout, left to right: 48px menu bar, sidebar (notebook list), editor panel, stream of posts. Dividers between panels are draggable.
  - The notebooks icon in the menu bar opens the sidebar. A close icon in the sidebar header closes it, and a closed sidebar takes no space. The sidebar starts closed, and the open or closed choice is remembered (`deep_sidebar_open` in localStorage).
  - Removed the "Notebooks" toggle button from the tag bar. Below 750px the menu bar becomes a horizontal strip above the stacked panels.
  - The menu bar sits below the header and tag bar rather than running the full height of the window.
  - Updated `docs/deep-two-panel-design-summary.md`.

- [x] **Remove the "New post…" box from the notebook view.** (2026-11-03, commit `a3c0f85` on `develop`)
  - Removed the `.composer` block from the right-hand stream panel, its CSS (`.composer`, `.composer-textarea`, `.composer-footer`, `.composer-hint`), the `postToStream()` handler, and the `composer-hint` reset in `openTag()`. The stream panel now shows only the post list.
  - Updated the right-panel line in `docs/deep-two-panel-design-summary.md`.
  - The home view's quick-compose editor (`POST /posts`) is now the only way to write posts from the UI.
  - Server endpoint left in place; see To do.

- [x] **Round the corners of panels and text areas.** (2026-11-03, commit `b382486` on `develop`)
  - Added `--radius: 6px` to the `:root` block in `ui/notebook.html` and applied it to `.doc-textarea` (notebook document and quick-compose), `.composer-textarea` (since removed), `.tag-input` (since removed) and `.post` (post cards, including the dashed failed state).
  - Not rounded: buttons (`.btn`, `.btn-logout`, `.post-merge`, `.back-link`), the toast, and the tag list items. Add `border-radius: var(--radius)` to them if a consistent look is wanted.

- [x] **Move the hardcoded colors to CSS variables.** (2026-11-03, commit `63a2850` on `develop`)
  - Added `--on-accent: #ffffff` (used by `.btn-primary`, `.btn-primary:hover:not(:disabled)`, `.post-merge:hover`) and `--danger-soft: #ff8888` (used by `.toast.error .toast-label` in `ui/toast.css`) to the `:root` block in `ui/notebook.html`.
  - No hardcoded colors remain outside `:root`. `deep-css-colors.xlsx` still lists these two as exceptions and has not been updated.