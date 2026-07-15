package directive

import (
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type typedCallout struct {
	Level string `directive:"level"`
	ast.BaseBlock
	Width int     `directive:"width"`
	Ratio float64 `directive:"ratio"`
	Wide  bool    `directive:"wide"`
}

var kindTypedCallout = ast.NewNodeKind("TypedCallout")

func (*typedCallout) Kind() ast.NodeKind { return kindTypedCallout }
func (n *typedCallout) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type typedMention struct {
	Account string `directive:"id"`
	Label   string `directive:",label"`
	ast.BaseInline
}

var kindTypedMention = ast.NewNodeKind("TypedMention")

func (*typedMention) Kind() ast.NodeKind { return kindTypedMention }
func (n *typedMention) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

func TestTypedRegistration(t *testing.T) {
	var h Handlers
	RegisterContainer[typedCallout](&h, "callout")
	RegisterText[typedMention](&h, "mention")

	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(
			util.Prioritized(NewDirectiveParser(), 50),
			util.Prioritized(NewCloseFenceParser(), 55),
			util.Prioritized(NewLeafDirectiveParser(), 60),
		),
		parser.WithInlineParsers(
			util.Prioritized(NewTextDirectiveParser(nil), 800),
		),
		parser.WithASTTransformers(util.Prioritized(NewTransformer(h), 100)),
	))
	src := []byte(":::callout{level=\"warn\" width=\"42\" wide=\"true\" ratio=\"1.5\"}\nbody\n:::\n\nping :mention[Patrick]{id=\"712020:abc\"}\n")
	root := md.Parser().Parse(text.NewReader(src))

	n := findNode(root, kindTypedCallout)
	if n == nil {
		t.Fatal("typed container not substituted")
	}
	c, ok := n.(*typedCallout)
	if !ok {
		t.Fatalf("wrong node type %T", n)
	}
	if c.Level != "warn" || c.Width != 42 || !c.Wide || c.Ratio != 1.5 {
		t.Errorf("attrs not bound: %+v", c)
	}
	if c.FirstChild() == nil {
		t.Error("container children not moved")
	}

	m := findNode(root, kindTypedMention)
	if m == nil {
		t.Fatal("typed text directive not substituted")
	}
	mention, ok := m.(*typedMention)
	if !ok {
		t.Fatalf("wrong node type %T", m)
	}
	if mention.Account != "712020:abc" || mention.Label != "Patrick" {
		t.Errorf("mention not bound: %+v", mention)
	}
}
