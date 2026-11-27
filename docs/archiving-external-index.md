# Archiving posts: external index vs. relations

Status: considered, not adopted (2026-11-26). Archive state stays in Hatcheck as the `archived` relation. See `archiving-handoff.md` for the current design.

## The idea

Store a post's archived flag outside Hatcheck, in a mutable, index-like structure (a "view" over the posts), instead of appending `archived` relations to Hatcheck's log. Unarchive would delete a row rather than append a new entry.

## Why it was considered

The relation log is append-only, so every archive or unarchive toggle adds an entry that is never removed. An external index would cap that growth.

## Assessment

**Storage is not the real cost.** A relation is a small log entry. Even 10,000 toggles come to a few hundred KB, and the rate is limited by how often a person clicks Archive. The real cost is on the read side: every notebook load reads the whole `archived` set and reduces it to "latest entry wins", so the work grows with total toggles, not with the number of archived posts.

**What an external index would give:**
- A mutable set, so "latest wins" goes away.
- Constant-time lookups, with no full read per notebook load.
- Room for extra fields such as a timestamp (relations have none).
- A clean separation: archiving is a view concern and does not touch the post's content or hash.

**What it would cost:**
- A second source of truth. Hatcheck backups, replication, or other Hatcheck clients would not know about archive state, and a lost or rebuilt index would make every post reappear.
- No history. The relation log records when posts were archived and unarchived; an index holds only the present state.
- New infrastructure. Deep is stdlib-only today. A persistent index means SQLite, bbolt, or an atomically written JSON file, plus handling concurrency and durability.
- Orphan hashes (entries for posts Hatcheck does not have) and, if Deep is multi-user, per-user keying.

## Decision

Keep the relation design. If notebook loads get slow, add a **derived in-memory cache** in Deep rather than a second store:

1. On startup (or first load), read `GET /relations?rel=deep:archived` once and reduce it to a set.
2. Update the set on every archive or unarchive write.
3. Notebook loads then consult the set and never read the relation log.
4. Optionally snapshot the set to a file for faster restarts. Losing it costs nothing, because it rebuilds from Hatcheck.

This keeps Hatcheck authoritative and removes the read cost. It does not shrink the log, but the log is not where the cost is.

## Revisit the external index if

- The log reaches hundreds of thousands of entries and even a one-time startup read is slow.
- Archive state should be private to Deep and invisible to other Hatcheck clients.
- Per-user archive state is needed without putting it in the shared log.
