# Deep: Two-Panel Design Summary

Oct 30, 2026 · @Steve

## Overview

Deep's UI is one page (`ui/notebook.html`, `develop` branch). From left to right it has a narrow menu bar, a sidebar, an editor panel and a stream of posts, with draggable dividers between the panels.

- **Menu bar:** a 48px vertical strip down the left edge, always visible. Its notebooks icon opens and closes the sidebar. When more menus exist, this is where the user picks which one the sidebar shows.
- **Sidebar:** the notebook list. It starts closed, opens from the menu bar icon, and closes with the close icon in its header. When closed it takes up no space. The open or closed choice is remembered across visits.
- **Main area:** the quick-compose editor (home) or the notebook (document plus post stream).
- **Layout:** a CSS grid (`240px 4px 1fr` with the sidebar open, `1fr` when closed), and `1fr 4px 1fr` inside a notebook.
- **Resizing:** dragging a divider resizes the panel to its left. Minimum widths keep a panel from collapsing (160px for the sidebar, 240px for the document).
- **Small screens:** below 750px the menu bar becomes a horizontal strip, the panels stack vertically, the dividers are hidden, and the left panel is capped at 40% of viewport height.

## Notebook view

Shown when a topic (`#tag`) is open: the left panel holds the document, and the right panel holds the posts that feed it.

- **Left panel (sticky):** the notebook's Markdown document in a plain textarea, with the `#tag` label above it and a status line and Save button below. The document is edited as Markdown source rather than in a rich text editor.
- **Right panel (scrolling):** the stream of every post carrying that tag, newest first. Each post shows its date, a "Merged" badge if it has been merged before, and a **Merge →** button.
- **Merge:** inserts the post's text into the document at the cursor, or over the current selection, adding paragraph breaks as needed. The status then reads "Merged — not yet saved."
- **Save:** creates a new immutable document version that supersedes the previous one and records which posts were merged in. The view then reloads, so the stream and badges refresh.

## Home view

This is the first screen whenever no notebook is open, so you can start writing without choosing a notebook first.

- **Sidebar (240px when open):** a scrollable list of known notebooks (tags), with `uncategorized` listed first.
- **Main area:** a quick-compose editor. Text with a `#tag` is stored as written, and untagged text gets `#uncategorized` appended.
- **Navigation:** there is no button back to this view from an open notebook; reloading the page returns here.

## Design intent

The left panel is where you work on the durable document, and the right panel is where raw posts accumulate. Moving text from right to left (Merge) is the core gesture.

Both views share one structure and one resizable divider, so switching between the home screen and a notebook feels consistent. The panels are plain and Markdown-based on purpose: Deep is experimental, and the merge model is still being explored.
