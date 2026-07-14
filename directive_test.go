package directive

import (
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func newTestParser() parser.Parser {
	md := goldmark.New(goldmark.WithParserOptions(
		parser.WithBlockParsers(
			util.Prioritized(NewDirectiveParser(), 50),
			util.Prioritized(NewCloseFenceParser(), 55),
			util.Prioritized(NewLeafDirectiveParser(), 60),
		),
		parser.WithInlineParsers(
			util.Prioritized(NewTextDirectiveParser(nil), 800),
		),
	))
	return md.Parser()
}

func parse(t *testing.T, src string) ast.Node {
	t.Helper()
	return newTestParser().Parse(text.NewReader([]byte(src)))
}

// findNode returns the first node of the given kind (depth first).
func findNode(root ast.Node, kind ast.NodeKind) ast.Node {
	var found ast.Node
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == kind {
			found = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return found
}

func TestContainerDirective(t *testing.T) {
	root := parse(t, ":::note[Label]{#id .a .b key=\"v\"}\ncontent\n:::\n")
	n := findNode(root, KindContainerDirective)
	if n == nil {
		t.Fatal("no container directive parsed")
	}
	cd := n.(*ContainerDirective)
	if cd.Name != "note" {
		t.Errorf("name %q", cd.Name)
	}
	if cd.Attrs["id"] != "id" || cd.Attrs["class"] != "a b" || cd.Attrs["key"] != "v" {
		t.Errorf("attrs %v", cd.Attrs)
	}
	// The label is the first child paragraph, tagged directiveLabel.
	first := cd.FirstChild()
	if first == nil {
		t.Fatal("no label paragraph")
	}
	if _, ok := first.AttributeString("directiveLabel"); !ok {
		t.Error("label paragraph not tagged")
	}
}

func TestContainerNesting(t *testing.T) {
	root := parse(t, "::::outer\n:::inner\nx\n:::\n::::\n")
	outer := findNode(root, KindContainerDirective).(*ContainerDirective)
	if outer.Name != "outer" {
		t.Fatalf("outer name %q", outer.Name)
	}
	inner := findNode(outer, KindContainerDirective)
	if inner == outer {
		inner = findNode(outer.FirstChild(), KindContainerDirective)
	}
	if inner == nil || inner.(*ContainerDirective).Name != "inner" {
		t.Error("inner container not nested")
	}
}

func TestLeafDirective(t *testing.T) {
	root := parse(t, "::media[shot.png]{width=\"80\"}\n")
	n := findNode(root, KindLeafDirective)
	if n == nil {
		t.Fatal("no leaf directive parsed")
	}
	ld := n.(*LeafDirective)
	if ld.Name != "media" || ld.Attrs["width"] != "80" {
		t.Errorf("leaf %q %v", ld.Name, ld.Attrs)
	}
}

func TestLeafTrailingContentInvalidates(t *testing.T) {
	root := parse(t, "::media[x] trailing\n")
	if findNode(root, KindLeafDirective) != nil {
		t.Error("trailing non-whitespace must invalidate the leaf directive")
	}
}

func TestTextDirective(t *testing.T) {
	root := parse(t, "a :status[Ready]{color=\"green\"} b\n")
	n := findNode(root, KindTextDirective)
	if n == nil {
		t.Fatal("no text directive parsed")
	}
	td := n.(*TextDirective)
	if td.Name != "status" || td.Attrs["color"] != "green" {
		t.Errorf("text %q %v", td.Name, td.Attrs)
	}
	if string(td.LabelSource) != "Ready" || td.LabelRoot == nil {
		t.Errorf("label %q root=%v", td.LabelSource, td.LabelRoot)
	}
}

func TestTextDirectiveGuards(t *testing.T) {
	for _, src := range []string{
		"a ::inline b\n",  // '::' in prose is not a text directive
		":smile: emoji\n", // name followed by ':' protects shortcodes
		"a \\:not b\n",    // escaped colon
		":bad- name\n",    // trailing '-' invalidates the name
	} {
		if findNode(parse(t, src), KindTextDirective) != nil {
			t.Errorf("%q must not parse a text directive", src)
		}
	}
}

func TestCloseFenceNeedsEnoughColons(t *testing.T) {
	// The 3-colon line cannot close a 4-colon container.
	root := parse(t, "::::note\nx\n:::\n")
	cd := findNode(root, KindContainerDirective).(*ContainerDirective)
	// The ::: line stays inside the (unclosed) container as content.
	if cd.NextSibling() != nil {
		t.Error("container should consume the short fence")
	}
}
