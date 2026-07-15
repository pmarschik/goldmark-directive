# goldmark-directive

remark-directive-compatible generic directives for goldmark. See README.md
for the syntax and usage; the compatibility target is the observed behavior
of remark-directive / micromark-extension-directive.

## Build & Test Commands

- `mise run setup` — install dependencies and hooks
- `mise run check` — run all quality gates (format + lint + typos + test)
- `mise run fmt` — format code
- `mise run lint` — run linters
- `mise run test` — run tests

## Conventions

### Commits

Use Conventional Commits strictly: `<type>(<scope>): <description>`.
Types: feat, fix, refactor, build, ci, chore, docs, style, perf, test.
Scopes: defined in `cog.toml`.

### API Stability

This is a public Go library. Breaking changes affect downstream consumers
(github.com/pmarschik/adfast and storysmith-md build on it).

- NEVER introduce breaking API changes without asking the user first
- Breaking changes MUST use `feat!:`/`fix!:` (major bump)
- Prefer adding over changing; deprecate before removing

### Behavior changes

Directive parsing is measured against remark-directive's actual output.
Do not change acceptance rules (name charset, emoji-shortcode guard,
close-fence colon counting, trailing-content invalidation) without a
matching remark-directive measurement.

### Version Control

- Primary VCS: jj (jujutsu), colocated with git
- Run `mise run check` before `jj git push`
- Do not push directly — prompt the user (hardware key signing)
