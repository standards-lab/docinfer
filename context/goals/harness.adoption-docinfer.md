# goal · harness.adoption-docinfer

- **State:** building
- **Task:** adopt
- **Branch:** adopt

## Tasks

1. [ ] adopt

## Task brief · adopt

**Problem**

docinfer doesn't run on marathon 0.22.0's harness. Its lints fail: a 14,000-word design note, a
goal record over budget, an unmarked capability map, and five references that don't resolve.
About 1,700 comment lines fall outside the five kinds, and the check runs neither lint.
v1.ai.inference's `docinfer` and `acceptance` tasks need a working context.

**Behaviors**
1. mise pins marathon 0.22.0 with `task.run_auto_install = false` and a `See` line; currency exits
   0.
2. The check runs lint-context and lint-references after the modules and sqlint, then fails if
   either failed. A lint missing from PATH prints `FAIL: <lint> is not on PATH; run mise install`.
   The check's description names both lints. CI's check job installs the marathon tools.
3. Both golangci-lint configurations require every suppression to name its linter and give a
   reason (nolintlint with require-explanation and require-specific).
4. The design note no longer exists: the built API is stated in package docs, with the failure
   table in the root package's; reasoning with no other home moves to docs/ pages, among it the
   join of work in flight and its singleflight rejection; cmd/docinfer's planned material moves,
   as written, to three notes under budget (the command, its API, the report).
5. Capability-map entries are a name, `(built)` or `(planned)`, and a link to the home. The roadmap
   pointer names `standards-lab/roadmap/index.toml`.
6. v1.ai.inference's record is within budget: the shipped postgres brief is gone; Decisions keep
   what binds `docinfer` and `acceptance`; its mentions of the design note and its pending edits
   name the new homes; its pending edit retiring docproc layers.md's docinfer sections is dropped,
   because docproc's adoption deletes those sections now.
7. lint-references passes: the test citations name the package docs' failure table, and the
   integration docs name their testdata README.
8. STANDARDS.md gains the comment rule, covering every configuration file, SQL headers as package
   documentation and template metadata, and frozen released migrations; its pointers name the new
   homes; it drops the L/W/G finding numbers.
9. Every comment in Go, tests, scripts, and configuration is one of the five kinds. Unexported and
   test-function doc comments go, their reasoning moving to exported docs, names, or docs/.
   currency.sh's sections become named functions; configuration headers are a short description
   plus `See` lines; SQL headers stay and migration 0001 stays byte-identical.
10. No CHANGELOG line and no tag.

**Test seams**
- `mise run check` (with both lints), `mise run currency`, and CI's check job.

**Slices**
1. Built design: package docs and docs/; lint-references passes.
2. Planned design: the three command notes; the design note is deleted; the map, the pointers,
   STANDARDS' pointers, and the record's mentions are updated.
3. v1.ai.inference record within budget. lint-context passes.
4. Pin, check, CI, suppression rule, currency.sh functions, configuration comments, STANDARDS
   comment rule.
5. Root-package comments.
6. Comments in chunk, join, job, docinfertest, integration, and the postgres module.

**Out of scope**

Building cmd/docinfer, and acceptance; docproc's layers.md; coordinator edits (pending edits);
README restructuring; releases.

**Door**

Two-way: docs, comments, and configuration only, with no tag and no change to a released migration.

## Progress

slices 6/6 committed · standards ✓ · spec ✓ · editor ✓

## Decisions

None.

## Pending edits

- coordinator · roadmap/v1.ai.inference.toml:8 and context/ai-strategy.md:76: "docinfer's
  `context/design.md` is its API's one home" becomes the package docs for the built API and the
  command notes for cmd/docinfer.
