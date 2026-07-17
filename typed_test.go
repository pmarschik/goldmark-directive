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

type typedFontSize struct {
	Label string `directive:",label"`
	ast.BaseInline
	Size int `directive:",value"`
}

var kindTypedFontSize = ast.NewNodeKind("TypedFontSize")

func (*typedFontSize) Kind() ast.NodeKind { return kindTypedFontSize }
func (n *typedFontSize) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type typedBreakout struct {
	Mode string `directive:",value"`
	ast.BaseBlock
}

var kindTypedBreakout = ast.NewNodeKind("TypedBreakout")

func (*typedBreakout) Kind() ast.NodeKind { return kindTypedBreakout }
func (n *typedBreakout) Dump(source []byte, level int) {
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

func valueTagParser(t *testing.T) parser.Parser {
	t.Helper()
	var h Handlers
	RegisterText[typedFontSize](&h, "fontSize")
	RegisterLeaf[typedBreakout](&h, "breakout")

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
	return md.Parser()
}

func TestValueTagBinding(t *testing.T) {
	p := valueTagParser(t)
	root := p.Parse(text.NewReader([]byte("big :fontSize[text]{14} words\n\n::breakout{wide}\n")))

	n := findNode(root, kindTypedFontSize)
	if n == nil {
		t.Fatal("typed text directive not substituted")
	}
	fs, ok := n.(*typedFontSize)
	if !ok {
		t.Fatalf("wrong node type %T", n)
	}
	if fs.Size != 14 {
		t.Errorf("bare value not bound to int: %+v", fs)
	}
	if fs.Label != "text" {
		t.Errorf(",label must still bind alongside ,value: %+v", fs)
	}

	b := findNode(root, kindTypedBreakout)
	if b == nil {
		t.Fatal("typed leaf directive not substituted")
	}
	bo, ok := b.(*typedBreakout)
	if !ok {
		t.Fatalf("wrong node type %T", b)
	}
	if bo.Mode != "wide" {
		t.Errorf("bare value not bound to string: %+v", bo)
	}
}

func TestValueTagMultipleBareAttrsStayZero(t *testing.T) {
	p := valueTagParser(t)
	root := p.Parse(text.NewReader([]byte("::breakout{a b}\n")))

	n := findNode(root, kindTypedBreakout)
	if n == nil {
		t.Fatal("typed leaf directive not substituted")
	}
	bo, ok := n.(*typedBreakout)
	if !ok {
		t.Fatalf("wrong node type %T", n)
	}
	if bo.Mode != "" {
		t.Errorf("two bare attrs must leave the ,value field zero: %+v", bo)
	}
}

type typedValueClash struct {
	Level string `directive:"level"`
	ast.BaseBlock
	Size int `directive:",value"`
}

var kindTypedValueClash = ast.NewNodeKind("TypedValueClash")

func (*typedValueClash) Kind() ast.NodeKind { return kindTypedValueClash }
func (n *typedValueClash) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

func TestValueTagValidationPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering a type mixing ,value with a named attr tag must panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "typedValueClash") {
			t.Errorf("panic must name the offending type: %v", r)
		}
	}()
	var h Handlers
	RegisterLeaf[typedValueClash](&h, "clash")
}
