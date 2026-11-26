# Deep design backlog

Open design changes for the Deep UI (`ui/notebook.html`, `ui/toast.css`).

## To do

- [ ] **Move the hardcoded colors to CSS variables.**
  - `#fff` (white text on blue buttons) appears in `.btn-primary`, `.btn-primary:hover` and `.post-merge:hover`. Suggested variable: `--on-accent`.
  - `#ff8888` (error label in the toast) is in `ui/toast.css`, in `.toast.error .toast-label`. Suggested variable: `--danger-soft`.
  - Why: every other color already comes from the `:root` block, so a theme, such as a dark one, would only need to override variables.
  - Reference: `deep-css-colors.xlsx` in this project lists the palette and these two exceptions.

- [ ] **Remove the "New post…" box from the notebook view.**
  - It is the `.composer` block in the right-hand stream panel, with its `postToStream()` handler.
  - Notes when picking this up: the home view's quick-compose editor is the other way to write posts. The `POST /notebook/{tag}/posts` endpoint, which forces the open tag onto the post, would no longer have a caller, so decide whether to keep or remove it.

## Done

(nothing yet)
