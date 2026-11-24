# Issue tracker

Issues and specs live in Markdown files under `.scratch/`.

## Publish and fetch

- Publish the spec to `.scratch/<feature-slug>/spec.md`.
- Publish one file per implementation or verification ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`. Start numbering at `01` for each feature. List prerequisites under `Blocked by:`; a ticket is unblocked when every prerequisite is `done`.
- Use a path to fetch a ticket, or resolve its number within the feature directory. Preserve links to the originating issue or evidence; publishing locally does not update GitHub.
- Version reviewed specs and tickets in Git. Before dispatching to another worktree, commit the inputs on the shared base branch so the worker can read them. Keep credentials and private test data out of these files.

## Close a ticket

When the acceptance criteria are verified, tick them, record the evidence (test command, commit, or manual result), and set `Status: done`. Use `ready-for-human` while a ticket still needs human verification. These are distinct from the `claimed` / `resolved` statuses used for decision tickets below.

## Triage

- Record the triage state near the top of the file in a `Status:` line. Use the strings in [Triage Labels](triage-labels.md). Set `Status: wontfix` to close a ticket through triage.
- Record the issue category in a `Category:` line with the value `bug` or `enhancement` when triage assigns one.
- Append comments and conversation history under `## Comments` at the bottom of the file.

## Wayfinding operations

- Store the map at `.scratch/<effort>/map.md`, with sections for Notes, Decisions so far, and Fog.
- Store each decision ticket at `.scratch/<effort>/issues/<NN>-<slug>.md`, numbering from `01`. Put the question in the body and record its type in `Type:` with the value `research`, `prototype`, `grilling`, or `task`.
- List dependencies near the top as `Blocked by: NN, NN`. A ticket is unblocked when all listed tickets have `Status: resolved`.
- Pick the lowest-numbered open, unblocked, unclaimed ticket. Save `Status: claimed` before starting work.
- To resolve a ticket, append its answer under `## Answer`, set `Status: resolved`, and add a short summary and a link to the ticket under Decisions so far in the map.
