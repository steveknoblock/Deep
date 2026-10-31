# Deep

A notebook app built on top of [Hatcheck](https://github.com/steveknoblock/hatcheck), a content-addressable object store.

Write short posts tagged with `#topics`. Each topic gets a **notebook**: a Markdown document that grows over time, next to a stream of every post carrying that tag. Pick posts from the stream, merge them into the document, and keep editing the Markdown directly.

Deep is a separate service, not part of Hatcheck. It talks to Hatcheck over plain HTTP, holds no data of its own, and keeps no credentials. Hatcheck stays a generic store (stashes, tags, names, relations); everything that makes a "notebook" lives here.

## How a notebook works

Deep adds no new storage types to Hatcheck. It composes the primitives Hatcheck already has:

- **A post** is an ordinary stash. A post written in a notebook always carries its `#tag`, so Hatcheck's tag index gathers the stream.
- **The document** is also an ordinary stash. It is distinguished only by the Hatcheck name `notebook/<tag>`, which points at its current version.
- **Saving the document** stashes the new text, points `notebook/<tag>` at it, and records a `supersedes` relation from the new version to the one before. Old versions are never changed or deleted.
- **Merging** records a `merged-from` relation from the new document version to each post that was merged in.
- **The stream** is every post with the tag, newest first, except the document's own versions. Without that exclusion a document that mentions its own tag would appear in its own stream.
- **The "Merged" badge** means a merge into some version of the document happened. It does not mean the post's text is still in the document, because later edits may have removed it.

Because every fact is an immutable relation between two objects, version history and merge tracking need no flags and no changes to the store.

## Running it

You need Go 1.25 or later and a running Hatcheck server.

```sh
git clone https://github.com/steveknoblock/Deep.git
cd Deep
cp .env.example .env         # then edit if needed
set -a && source .env && set +a
go run ./server
```

`deep.sh` runs Deep in the background as a process named `deep-server`, so it is easy to find in `ps` or `top`:

```sh
./deep.sh start      # rebuilds first if the Go code changed
./deep.sh start -f   # also stops whatever is holding Deep's port
./deep.sh stop
./deep.sh restart
./deep.sh status
./deep.sh logs
```

It reads `.env` for the same settings and writes its log to `deep.log`.

`start.sh` does the same after pulling the latest code and building. It expects the project at `~/projects/Deep`, so adjust the `cd` line if yours is elsewhere.

Deep listens on port 8091 by default and serves its own UI at `http://localhost:8091/ui/notebook.html`.

### Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `DEEP_PORT` | `8091` | Port Deep listens on. |
| `HATCHECK_URL` | `http://localhost:8090` | Base URL of the Hatcheck server Deep calls. |
| `DEEP_UI` | `./ui` | Directory of static UI files Deep serves at `/ui/`. |

## Signing in

Deep has no login of its own. Sign in to Hatcheck as usual (magic link), then click the **Notebook →** button in Hatcheck's UI. Hatcheck hands your session and capability token to Deep in the URL, and Deep keeps them in its own browser session storage. Hatcheck and Deep run on different origins, which is why the hand-off is needed.

Deep never validates a session itself. It forwards the browser's token to Hatcheck on every call, so Hatcheck decides what you may read and write. An expired or invalid session shows up as a 401 from Hatcheck.

## HTTP API

Every call needs `Authorization: Bearer <session JWT>`, and may carry `X-Capability-Token` with the caller's Hatcheck capability token. Request bodies are plain text, and responses are JSON.

| Method and path | Purpose |
| --- | --- |
| `GET /notebooks` | List the tags that have a notebook document. `uncategorized` is always listed first. |
| `GET /notebook/{tag}` | The notebook: current document (if any) and the post stream. |
| `POST /notebook/{tag}/document` | Save a new version of the document. The body is the Markdown. An optional `X-Merged-Posts` header lists, comma separated, the post hashes merged in. |
| `POST /notebook/{tag}/posts` | Create a post in the notebook. The `#tag` is added if the text lacks it. |
| `POST /posts` | Create a quick note. Text that mentions a `#tag` is stored as written; text with no tag gets `#uncategorized` appended. |
| `/ui/` | The web UI. |

## Layout

```
server/                  HTTP server, routes and handlers
internal/hatcheckclient/ Thin client for Hatcheck's HTTP API
internal/notebook/       The notebook logic: documents, versions, streams
ui/                      The browser UI
```

## Status

Experimental. Deep exists to try ideas for an application on top of Hatcheck, and the merge model is still being explored. The document is edited as Markdown source on purpose, rather than in a rich text editor.