# Deep: Two-Panel Design Summary

Oct 30, 2026 · @Steve

## Overview

Deep's UI is one page (`ui/notebook.html`, `develop` branch) with two views that share the same two-panel layout: a left panel, a 4px draggable divider, and a right panel.

- **Layout:** a CSS grid (`1fr 4px 1fr` for notebooks, `240px 4px 1fr` on the home view).
- **Resizing:** dragging the divider resizes the panels. Minimum widths keep either panel from collapsing (160px on home, 240px in a notebook).
- **Small screens:** below 750px the panels stack vertically, the divider is hidden, and the left panel is capped at 40% of viewport height.

## Notebook view

Shown when a topic (`#tag`) is open: the left panel holds the document, and the right panel holds the posts that feed it.

- **Left panel (sticky):** the notebook's Markdown document in a plain textarea, with the `#tag` label above it and a status line and Save button below. The document is edited as Markdown source rather than in a rich text editor.
- **Right panel (scrolling):** a composer for new posts at the top, then the stream of every post carrying that tag, newest first. Each post shows its date, a "Merged" badge if it has been merged before, and a **Merge →** button.
- **Merge:** inserts the post's text into the document at the cursor, or over the current selection, adding paragraph breaks as needed. The status then reads "Merged — not yet saved."
- **Save:** creates a new immutable document version that supersedes the previous one and records which posts were merged in. The view then reloads, so the stream and badges refresh.

## Home view

This is the first screen whenever no notebook is open, so you can start writing without choosing a notebook first.

- **Left panel (narrow, 240px):** a scrollable list of known notebooks (tags), with `uncategorized` listed first.
- **Right panel (wide):** a quick-compose editor. Text with a `#tag` is stored as written, and untagged text gets `#uncategorized` appended.
- **Navigation:** the **All notebooks** button returns here from any open notebook.

## Design intent

The left panel is where you work on the durable document, and the right panel is where raw posts accumulate. Moving text from right to left (Merge) is the core gesture.

Both views share one structure and one resizable divider, so switching between the home screen and a notebook feels consistent. The panels are plain and Markdown-based on purpose: Deep is experimental, and the merge model is still being explored.
