# Operating a published mirror

Re-running builds, recovering a tree Cairndex will no longer write to, checking
what you published, and shipping only what moved.

## Re-running, and what happens when files go away

Builds are repeatable. Cairndex records what it generated in `.cairndex-manifest.json`,
replaces its own output on a later run, and still refuses to touch a file it did
not create.

Removals are handled too. Anything the previous run wrote and this one did not is
deleted, and a directory left holding nothing goes with it — so a removed file
loses its digest, and a removed directory loses the whole listing published for
it rather than serving a page of links to things that are gone.

Only paths the manifest recorded are ever considered, so Cairndex can delete nothing
it did not create. Two consequences worth knowing:

- **Losing the manifest stops the build.** An `rsync --delete` or a `git clean`
  over the output directory is enough, as is a manifest written by a Cairndex old
  enough to have recorded output in a shape this one does not read. Cairndex will
  not overwrite files it can no longer prove it wrote. Recover with
  `cairndex build --adopt`, below. The run says `the manifest could not be read`
  before anything else — without that line the conflicts that follow name a path
  and say it already exists, which is true and points at the wrong thing
  entirely.
- **Two configs must not share one output root.** Each would prune the other's
  files, and there is no way to tell that apart from a directory that was
  legitimately removed.

- **A build that dies partway is recoverable.** Cairndex records what it managed to
  write before the error, alongside everything the previous run claimed. Without
  that the partial output would belong to nobody and `on_conflict: error` would
  refuse every later run until someone deleted the files by hand.

The manifest is replaced by a rename rather than rewritten in place, so a build
interrupted mid-save leaves either the old manifest or the new one and never a
half-written file. That matters more than it sounds: a manifest that will not
parse is read as Cairndex claiming nothing, and the next build then refuses every
file the last one wrote as somebody else's and prunes none of it. The hash cache
is written the same way, where the cost of losing it is a full re-hash.

A build also drops the cache records for files that are no longer in the tree,
and reports the count as `forgot`. Nothing used to remove one, so a mirror that
churns accumulated an entry for every file that had ever been in it — on a tree
of two million files, a cache of hundreds of megabytes, read and rewritten on
every build, almost all of it describing files that are gone. Only the region
the run actually rebuilt is swept: a full build sweeps from `root:`, and a
`cairndex watch` rebuild sweeps its own subtree, so the digests for the rest of the
mirror survive a change instead of being re-computed on the next full build. A
build that failed sweeps nothing, because it never reached the rest of its
scope.

### Getting a wedged tree back

Ownership has two states, and one edge leads back out of the bad one.

![A state diagram. A first build reaches Owned, where the manifest records every
file Cairndex wrote. An rsync --delete, a git clean, or a manifest this Cairndex cannot
parse moves it to Unclaimed, where the output is still there and the manifest is
not. From Unclaimed a plain build is refused on the first file, and a build with
on_conflict skip writes nothing and claims nothing; both return to Unclaimed
without changing the tree. Only build --adopt leads from Unclaimed back to
Owned.](../diagrams/ownership.svg)

The two boxes along the bottom are the trap. They are what the first two
remedies an operator reaches for actually do, and both loop straight back:
nothing about the tree has changed.

The diagram is generated — `docs/diagrams/ownership.puml` is the source, and
`make diagrams` re-renders it.

```sh
cairndex build --dry-run --adopt   # read what it would take
cairndex build --adopt             # take it
```

`--adopt` claims output paths that already exist and Cairndex does not own, instead
of refusing them. It exists for one state: the manifest is gone or unreadable,
so every file Cairndex wrote is a file it no longer claims, and `on_conflict: error`
refuses all of them.

There was no way out of that before. Deleting the output is not one where `root:`
and `out:` are the same directory, because that deletes the artifacts. Neither is
`on_conflict: skip` — it leaves each conflicting path alone, so nothing is
written and nothing is ever claimed, and the mirror is frozen at whatever it held
when the manifest was lost.

What it claims is exactly the set of paths this build produces that already
exist. It never walks `out:` looking for files that seem generated, so it cannot
take one this build does not itself write, and `protect:` and path containment
are checked ahead of it and are not affected. Every claim is reported at warning
level with a count, because waiving the conflict check is not something to
discover afterwards.

One thing it does not do: output from an earlier config that this build no longer
generates stays unclaimed, and is therefore never pruned — `Prune` only removes
what the manifest records. `cairndex check` reports those as output Cairndex does not
own, and `cairndex check --remove-orphaned` deletes the ones it can show are Cairndex's
once you have read the list. That flag refuses while the manifest claims nothing,
since everything generated looks unowned in that state; adopt first, then remove.

Expect it to leave some behind, and expect that rather than reading it as a
failed removal. The report is name-based, which is what makes it useful in a
mirror; deletion is content-based, because in a mirror a foreign `index.html` is
an ordinary thing to find and losing it is unrecoverable. A file is removed only
when its own bytes identify it — a listing's shape, Cairndex's CSV header,
`_index.md` frontmatter, or the `generator` meta tag in a page Cairndex rendered.

`index.txt` and `SHA256SUMS` never qualify: one filename per line is what any
listing looks like, and `SHA256SUMS` is coreutils format, so Cairndex's is
indistinguishable from the one a Debian or release mirror ships. Stale ones
survive every `--remove-orphaned` run, stay in the report, and keep the check
exiting non-zero until you delete them yourself. That last part is worth knowing
in a pipeline: a run that deleted everything it could still exits non-zero when
anything was kept.

Pages published before the `generator` meta tag existed do not carry it, and a
rebuild will not give them one: Cairndex writes the paths it currently generates,
and a stale page is by definition at a path it no longer does. Those are kept
permanently and have to be deleted by hand. Only HTML written by a version that
emits the marker can be removed automatically later, so this is a one-time cost
against trees published before it.

### Seeing what a run would do first

```sh
cairndex build --dry-run
```

Every decision runs against the tree as it stands — what would be written, which
of those bodies differ from what is on disk, and every path `Prune` would
delete — and nothing under `out:` changes, including the manifest and the hash
cache. Reach for it after moving `out:`, after changing `index_basename` or
`outputs:`, and any time a build is about to run against a directory whose
manifest you are not sure of.

`--changed-to` still writes the file it names, so the deployment's transfer list
can be read before anything moves:

```sh
cairndex build --dry-run --changed-to /tmp/would-change.txt
```

## Verifying a published mirror

`cairndex check` reads back what a build recorded. It re-hashes every file
`SHA256SUMS` names, reports what the manifest claims and the disk no longer has,
finds output Cairndex does not own, and catches its own output being changed
after it was written.

```sh
cairndex check --config cairndex.yaml
```

The third finding is the one nothing else can produce. `sha256sum -c` confirms
the artifacts a client was told about; only the manifest knows which files Cairndex
actually wrote, so only Cairndex can tell a current index from one left behind when
`index_basename` or `outputs:` changed. Stale output is the dangerous kind — it
is still served, it still looks authoritative, and it describes a directory as
it was.

That last one needs the manifest's digests, and nothing else can see it.
Generated files appear in no `SHA256SUMS` — a listing leaves Cairndex's own output
out, or the build would never reach a fixed point — so a hand-edited
`index.json` is invisible to a client verifying checksums. The watcher cannot
see it either: it discards events on its own output by name, which is what stops
a rebuild loop, and a name cannot tell Cairndex's write from anyone else's. Running
`build` again repairs it.

Nothing is repaired. An operator unsure about a mirror needs to know what changed
before anything touches it, and a command that fixes what it finds cannot be run
to answer that question. A check that fails exits non-zero, so a deploy can
refuse to publish.

Two things it deliberately does not do. It never consults `.cairndex-cache.json`:
that cache is keyed by path, size and mtime, all three of which survive a
same-size edit with the timestamp restored, so it is the wrong oracle for a
tamper check. And it never follows a symlink — one standing where a file is
claimed is a finding, not something to hash through.

## Publishing only what moved

A rebuild of an unchanged tree writes nothing: identical output is skipped, and
`Listing.generated` holds the newest modification time among the entries rather
than the build clock, so two builds of one tree produce identical bytes.

That makes a delta deploy possible, and `--changed-to` is how a deployment finds
out which files those are:

```sh
cairndex build --config cairndex.yaml --changed-to /tmp/changed.txt
rsync -a --files-from=/tmp/changed.txt out/ mirror:/srv/mirror/
```

The format is rsync's `--files-from`: paths relative to the output directory,
one per line. A build that changed nothing writes an empty file rather than no
file, so a script can tell "nothing moved" from "the build never ran".

Deletions are not in the list. `Prune` has already removed them from the output
directory, so a sync of that directory carries them — a `--files-from` transfer
does not, and needs its own `--delete` pass.

## Exit codes

`build`, `watch` and `check` all exit `130` (`128 + SIGINT`, the shell's own
convention) when a Ctrl-C lands before they are done — an operator's own
interruption, not a failure to investigate. Every other error is `1`. `0` is
success.

This is deliberately not finer-grained than that: a bad config, a permission
error and a build that failed partway through are all `1`, distinguished
from each other by the log line on stderr rather than by the code, since a
script branching on "did this need a human" only ever needed the one real
split — interrupted versus broken.

One case reads as `0` on purpose, not `130`: `watch`'s own steady-state loop
(after the first build has already succeeded) treats being told to stop as
the expected end of "watches until interrupted", not as a build cut short — a
SIGINT to a long-running `watch --serve` exits `0`. Only an interruption
*before* a build finishes — a plain `build`, `check`, or `watch`'s own first
build — is `130`.

SIGINT and SIGTERM are handled identically, and every command claims both.
SIGTERM is what `docker stop`, `systemctl stop` and a Kubernetes pod shutdown
send, so a container takes it on every routine stop; registered for neither,
the Go runtime's default action would end the process mid-write. A SIGTERM
that interrupts a run reports `130` rather than `143` — nothing downstream
knows which signal cancelled the context, and the split this code draws is
interrupted-versus-broken. See [Stopping the container](container.md).
