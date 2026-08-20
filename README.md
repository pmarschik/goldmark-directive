# goldmark-directive

[remark-directive](https://github.com/remarkjs/remark-directive)-compatible
generic directives for [goldmark](https://github.com/yuin/goldmark):

```markdown
:::name[label]{#id .class key="value"}
container content
:::

::name[label]{attrs} leaf (single-line block)

inline :name[label]{attrs} directives
```

The compatibility target is the **observed behavior** of remark-directive
(micromark-extension-directive), including its edge rules:

- names start alphanumeric, may contain `-`/`_`, but must not end with them
- a text directive fails after `:`, after an ODD run of backslashes (the
  last one is still an escape marker; an even run is literal backslashes
  and the directive opens), and when the name is directly followed by `:`
  (protects `:emoji:` shortcodes)
- trailing non-whitespace on a container/leaf marker line invalidates it
- close fences need at least as many colons as the opening fence
- container labels become the first child paragraph (tagged with the
  `directiveLabel` attribute); leaf/text labels become inline children

## Setup

One call registers everything:

```go
md := goldmark.New(goldmark.WithExtensions(&directive.Extension{}))
root := md.Parser().Parse(text.NewReader(source))
```

`Extension` optionally takes `Handlers` (typed directives, below),
`RestrictToHandlers` (parse only the registered names, per kind — anything
else stays literal text; `AllowedNames` does the same with an explicit
list), and `LabelParser` (the nested parser for text-directive labels;
pass your document parser factory so labels support the same inline
constructs). Note the registry alone only _types_ nodes — without a
restriction every syntactically valid directive still parses generically,
matching remark.

<details>
<summary>Manual wiring (custom priorities / per-kind allowlists)</summary>

Four parsers exist because goldmark separates block from inline parsing,
and the container close fence must interrupt open paragraphs through the
regular block-open machinery:

```go
md := goldmark.New(goldmark.WithParserOptions(
    parser.WithBlockParsers(
        util.Prioritized(directive.NewDirectiveParser(), 50),
        util.Prioritized(directive.NewCloseFenceParser(), 55),
        util.Prioritized(directive.NewLeafDirectiveParser(), 60),
    ),
    parser.WithInlineParsers(
        util.Prioritized(directive.NewTextDirectiveParser(nil), 800),
    ),
))
```

Every constructor accepts `directive.WithAllowedNames(...)` for per-kind
restriction.

</details>

## Adding a directive of each kind

Every syntactically valid directive parses into a generic AST node
(`ContainerDirective`, `LeafDirective`, `TextDirective` — each with `Name`
and `Attrs`). To implement a _specific_ directive, declare a node type with
`directive:"…"` tags and register it — attributes bind automatically
(string, bool, ints, floats; `#id`/`.class` shorthands arrive as the `id`
and `class` attributes).

### Container directive — `:::callout`

```markdown
:::callout{level="warn" width="42"}
Careful with that axe.
:::
```

```go
type Callout struct {
    ast.BaseBlock
    Level string `directive:"level"`
    Width int    `directive:"width"`
}

var KindCallout = ast.NewNodeKind("Callout")

func (*Callout) Kind() ast.NodeKind { return KindCallout }
func (n *Callout) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

var h directive.Handlers
directive.RegisterContainer[Callout](&h, "callout")
```

The container's children (including the label paragraph, tagged with the
`directiveLabel` attribute) move into your node automatically.

### Leaf directive — `::youtube`

```markdown
::youtube[An intro video]{id="dQw4w9WgXcQ"}
```

```go
type YouTube struct {
    ast.BaseBlock
    VideoID string `directive:"id"`
}

directive.RegisterLeaf[YouTube](&h, "youtube")
```

### Text directive — `:mention`

```markdown
Ping :mention[Patrick]{id="712020:abc"} about this.
```

```go
type Mention struct {
    ast.BaseInline
    Account string `directive:"id"`
    Label   string `directive:",label"`
}

directive.RegisterText[Mention](&h, "mention")
```

The `directive:",label"` tag binds the raw label text; `LabelRoot` on the
generic node holds the parsed label inlines when you need structure.

### Bare-value attributes — `directive:",value"`

A `{value}` attribute block with a single bare token (`{14}`, `{wide}`)
parses as a key with an empty value. Tag one field `directive:",value"` to
bind that token directly (same conversions as named attrs):

```markdown
Use :fontSize[bigger text]{14} sparingly.
```

```go
type FontSize struct {
    ast.BaseInline
    Size  int    `directive:",value"`
    Label string `directive:",label"`
}

directive.RegisterText[FontSize](&h, "fontSize")
```

A `,value` field must be the **only** attr-binding `directive:` tag on the
struct (`,label` is still fine — it binds the label, not attributes);
registration panics otherwise. With zero or multiple bare tokens (`{a b}`)
the field stays zero.

### Wiring it up

```go
md := goldmark.New(goldmark.WithExtensions(&directive.Extension{
    Handlers:           h,
    RestrictToHandlers: true, // parse ONLY registered names, per kind
}))
```

When binding isn't enough (computed fields, validation, label inspection),
register a plain handler instead — it receives the generic node and returns
the replacement (`nil` keeps the generic node):

```go
h.Container = map[string]func(*directive.ContainerDirective) ast.Node{
    "callout": func(d *directive.ContainerDirective) ast.Node {
        n := &Callout{}
        directive.BindAttrs(n, d.Attrs)
        if n.Level == "" { n.Level = "info" }
        return n
    },
}
```

Rendering is up to you — register `renderer.NodeRenderer` implementations
for your node kinds (this library does not ship HTML renderers).

## Source spans

Every generic directive node records where it came from in a `Span`
(`text.Segment`), so `src[n.Span.Start:n.Span.Stop]` is the directive's own
source text. Offsets are **byte** offsets into the parsed source, and line
terminators are excluded.

| node                 | `Span` covers                         |
| -------------------- | ------------------------------------- |
| `LeafDirective`      | the whole `::name[label]{attrs}` line |
| `TextDirective`      | the whole `:name[label]{attrs}` run   |
| `CloseFence`         | the `:::` fence line                  |
| `ContainerDirective` | the **opening fence line only**       |

`ContainerDirective` is the exception: its end is not known when the block
opens, so the full extent runs from `Span.Start` to the matching
`CloseFence`'s `Span.Stop`. An unclosed container has no `CloseFence` at
all — consumers decide its end themselves (last child, or end of input).

## Development

Tooling is managed with [mise](https://mise.jdx.dev): `mise run setup` once,
then `mise run check` (format + lint + typos + test) before pushing. Commits
follow Conventional Commits; see AGENTS.md for contributor guidelines.
