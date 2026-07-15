package directive

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Option configures a directive parser.
type Option func(*parserConfig)

type parserConfig struct {
	allowed map[string]bool
}

// WithAllowedNames restricts a parser to the given directive names —
// anything else stays literal text instead of parsing as a generic
// directive. Without this option every syntactically valid name parses
// (remark-directive's behavior). Applying the option twice intersects
// the lists (a name must pass every restriction).
func WithAllowedNames(names ...string) Option {
	return func(c *parserConfig) {
		set := make(map[string]bool, len(names))
		for _, n := range names {
			set[n] = true
		}
		if c.allowed == nil {
			c.allowed = set
			return
		}
		for k := range c.allowed {
			if !set[k] {
				delete(c.allowed, k)
			}
		}
	}
}

func (c *parserConfig) accepts(name string) bool {
	return c.allowed == nil || c.allowed[name]
}

func applyOptions(opts []Option) *parserConfig {
	c := &parserConfig{}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Handlers maps directive names to node constructors, per kind. A handler
// receives the parsed generic node and returns its replacement; returning
// nil keeps the generic node in place.
type Handlers struct {
	Container map[string]func(*ContainerDirective) ast.Node
	Leaf      map[string]func(*LeafDirective) ast.Node
	Text      map[string]func(*TextDirective) ast.Node
}

// NewTransformer returns a goldmark ASTTransformer that replaces
// registered directives with handler-produced nodes. For container
// directives the original node's children (including the label paragraph)
// move into the replacement automatically.
//
// Register it with parser.WithASTTransformers(util.Prioritized(t, prio)).
func NewTransformer(h Handlers) parser.ASTTransformer {
	return &transformer{handlers: h}
}

type transformer struct {
	handlers Handlers
}

func (t *transformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	t.walk(doc)
}

// walk replaces registered directives depth-first. It iterates siblings by
// capturing the next node BEFORE a replacement detaches the current one
// (ast.Walk would lose the remaining siblings), and recurses into
// replacements so directives inside moved children still transform.
func (t *transformer) walk(n ast.Node) {
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		if repl := t.replacement(c); repl != nil {
			if _, ok := c.(*ContainerDirective); ok {
				for child := c.FirstChild(); child != nil; {
					cn := child.NextSibling()
					repl.AppendChild(repl, child)
					child = cn
				}
			}
			n.ReplaceChild(n, c, repl)
			t.walk(repl)
		} else {
			t.walk(c)
		}
		c = next
	}
}

// replacement runs the registered handler for a directive node; nil means
// "leave the node alone" (no handler, or the handler declined).
func (t *transformer) replacement(n ast.Node) ast.Node {
	switch d := n.(type) {
	case *ContainerDirective:
		if fn := t.handlers.Container[d.Name]; fn != nil {
			return fn(d)
		}
	case *LeafDirective:
		if fn := t.handlers.Leaf[d.Name]; fn != nil {
			return fn(d)
		}
	case *TextDirective:
		if fn := t.handlers.Text[d.Name]; fn != nil {
			return fn(d)
		}
	}
	return nil
}
