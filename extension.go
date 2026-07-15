package directive

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/util"
)

// Extension registers everything in one call:
//
//	md := goldmark.New(goldmark.WithExtensions(&directive.Extension{
//	    Handlers: handlers,
//	}))
//
// Four parsers exist because goldmark separates block from inline parsing
// and the container close fence must interrupt open paragraphs through the
// regular block-open machinery — the Extension hides that wiring. It also
// registers the Handlers transformer when set.
//
// Note: this library ships no HTML renderers. Parse-only use
// (md.Parser().Parse(...)) works out of the box; goldmark.Convert needs
// renderer.NodeRenderer registrations for any directive nodes that survive
// rendering.
type Extension struct {
	Handlers           Handlers
	LabelParser        func() parser.Parser
	AllowedNames       []string
	RestrictToHandlers bool
}

// Extend implements goldmark.Extender.
func (e *Extension) Extend(m goldmark.Markdown) {
	var shared []Option
	if e.AllowedNames != nil {
		shared = append(shared, WithAllowedNames(e.AllowedNames...))
	}
	container, leaf, txt := shared, shared, shared
	if e.RestrictToHandlers {
		container = append(container[:len(container):len(container)], WithAllowedNames(mapKeys(e.Handlers.Container)...))
		leaf = append(leaf[:len(leaf):len(leaf)], WithAllowedNames(mapKeys(e.Handlers.Leaf)...))
		txt = append(txt[:len(txt):len(txt)], WithAllowedNames(mapKeys(e.Handlers.Text)...))
	}
	m.Parser().AddOptions(
		parser.WithBlockParsers(
			util.Prioritized(NewDirectiveParser(container...), 50),
			util.Prioritized(NewCloseFenceParser(), 55),
			util.Prioritized(NewLeafDirectiveParser(leaf...), 60),
		),
		parser.WithInlineParsers(
			util.Prioritized(NewTextDirectiveParser(e.LabelParser, txt...), 800),
		),
	)
	if e.Handlers.Container != nil || e.Handlers.Leaf != nil || e.Handlers.Text != nil {
		m.Parser().AddOptions(parser.WithASTTransformers(
			util.Prioritized(NewTransformer(e.Handlers), 100),
		))
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
