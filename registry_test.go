package directive

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func TestWithAllowedNames(t *testing.T) {
	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(
			util.Prioritized(NewDirectiveParser(WithAllowedNames("note")), 50),
			util.Prioritized(NewCloseFenceParser(), 55),
			util.Prioritized(NewLeafDirectiveParser(WithAllowedNames("media")), 60),
		),
		parser.WithInlineParsers(
			util.Prioritized(NewTextDirectiveParser(nil, WithAllowedNames("mention")), 800),
		),
	))
	p := md.Parser()

	root := p.Parse(text.NewReader([]byte(":::note\nx\n:::\n")))
	if findNode(root, KindContainerDirective) == nil {
		t.Error("allowed container must parse")
	}
	root = p.Parse(text.NewReader([]byte(":::other\nx\n:::\n")))
	if findNode(root, KindContainerDirective) != nil {
		t.Error("unlisted container must stay text")
	}
	root = p.Parse(text.NewReader([]byte("::youtube[x]\n")))
	if findNode(root, KindLeafDirective) != nil {
		t.Error("unlisted leaf must stay text")
	}
	root = p.Parse(text.NewReader([]byte("a :status[x] b\n")))
	if findNode(root, KindTextDirective) != nil {
		t.Error("unlisted text directive must stay text")
	}
	root = p.Parse(text.NewReader([]byte("a :mention[x] b\n")))
	if findNode(root, KindTextDirective) == nil {
		t.Error("allowed text directive must parse")
	}
}

// calloutBlock is a sample custom node for transformer tests.
type calloutBlock struct {
	Level string
	ast.BaseBlock
}

var kindCallout = ast.NewNodeKind("Callout")

func (*calloutBlock) Kind() ast.NodeKind { return kindCallout }
func (n *calloutBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

func TestNewTransformer(t *testing.T) {
	handlers := Handlers{
		Container: map[string]func(*ContainerDirective) ast.Node{
			"note": func(d *ContainerDirective) ast.Node {
				return &calloutBlock{Level: d.Attrs["level"]}
			},
		},
	}
	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(
			util.Prioritized(NewDirectiveParser(), 50),
			util.Prioritized(NewCloseFenceParser(), 55),
			util.Prioritized(NewLeafDirectiveParser(), 60),
		),
		parser.WithASTTransformers(util.Prioritized(NewTransformer(handlers), 100)),
	))
	root := md.Parser().Parse(text.NewReader([]byte(":::note{level=\"warn\"}\ncontent here\n:::\n")))

	n := findNode(root, kindCallout)
	if n == nil {
		t.Fatal("handler node not substituted")
	}
	callout, ok := n.(*calloutBlock)
	if !ok || callout.Level != "warn" {
		t.Fatalf("attrs not carried: %+v", n)
	}
	// Children moved into the replacement.
	var sb strings.Builder
	src := []byte(":::note{level=\"warn\"}\ncontent here\n:::\n")
	for c := callout.FirstChild(); c != nil; c = c.NextSibling() {
		if p, ok := c.(*ast.Paragraph); ok {
			for i := range p.Lines().Len() {
				seg := p.Lines().At(i)
				sb.Write(seg.Value(src))
			}
		}
	}
	if !strings.Contains(sb.String(), "content here") {
		t.Errorf("children not moved: %q", sb.String())
	}
	if findNode(root, KindContainerDirective) != nil {
		t.Error("generic node should be replaced")
	}
}
