# goldmark-directive

[remark-directive](https://github.com/remarkjs/remark-directive)-compatible
generic directives for [goldmark](https://github.com/yuin/goldmark):

```markdown
:::name[label]{#id .class key="value"}
container content
:::

::name[label]{attrs}      leaf (single-line block)

inline :name[label]{attrs} directives
```

The compatibility target is the **observed behavior** of remark-directive
(micromark-extension-directive), including its edge rules:

- names start alphanumeric, may contain `-`/`_`, but must not end with them
- a text directive fails after `:` or `\`, and when the name is directly
  followed by `:` (protects `:emoji:` shortcodes)
- trailing non-whitespace on a container/leaf marker line invalidates it
- close fences need at least as many colons as the opening fence
- container labels become the first child paragraph (tagged with the
  `directiveLabel` attribute); leaf/text labels become inline children

## Usage

Register the parsers directly (priorities shown are the ones the library is
tested with):

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

Text-directive labels are parsed with a nested parser; pass your document
parser factory to `NewTextDirectiveParser` so labels support the same inline
constructs, or `nil` for a minimal default.

The AST nodes are `ContainerDirective`, `LeafDirective`, and `TextDirective`
(with `Name`, `Attrs`, and label access); `DirectiveCloseFence` is an inert
marker consumers should ignore.
