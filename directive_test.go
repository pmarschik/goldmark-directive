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
	if err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == kind {
			found = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	}); err != nil {
		panic(err)
	}
	return found
}

// collectNodes returns every node of the given kind in document order.
func collectNodes(root ast.Node, kind ast.NodeKind) []ast.Node {
	var found []ast.Node
	if err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == kind {
			found = append(found, n)
		}
		return ast.WalkContinue, nil
	}); err != nil {
		panic(err)
	}
	return found
}

// spanText slices src with a node's Span, so span assertions read as the
// expected substring instead of magic offsets.
func spanText(src string, span text.Segment) string {
	return string(span.Value([]byte(src)))
}

func TestContainerDirective(t *testing.T) {
	root := parse(t, ":::note[Label]{#id .a .b key=\"v\"}\ncontent\n:::\n")
	n := findNode(root, KindContainerDirective)
	if n == nil {
		t.Fatal("no container directive parsed")
	}
	cd, ok := n.(*ContainerDirective)
	if !ok {
		t.Fatalf("wrong node type %T", n)
	}
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
	outer, ok := findNode(root, KindContainerDirective).(*ContainerDirective)
	if !ok || outer.Name != "outer" {
		t.Fatalf("outer name %q", outer.Name)
	}
	inner := findNode(outer, KindContainerDirective)
	if inner == outer {
		inner = findNode(outer.FirstChild(), KindContainerDirective)
	}
	innerCD, ok := inner.(*ContainerDirective)
	if !ok || innerCD.Name != "inner" {
		t.Error("inner container not nested")
	}
}

func TestLeafDirective(t *testing.T) {
	root := parse(t, "::media[shot.png]{width=\"80\"}\n")
	n := findNode(root, KindLeafDirective)
	if n == nil {
		t.Fatal("no leaf directive parsed")
	}
	ld, ok := n.(*LeafDirective)
	if !ok {
		t.Fatalf("wrong node type %T", n)
	}
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
	td, ok := n.(*TextDirective)
	if !ok {
		t.Fatalf("wrong node type %T", n)
	}
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
	cd, ok := findNode(root, KindContainerDirective).(*ContainerDirective)
	if !ok {
		t.Fatal("no container directive parsed")
	}
	// The ::: line stays inside the (unclosed) container as content.
	if cd.NextSibling() != nil {
		t.Error("container should consume the short fence")
	}
}

// ---------------------------------------------------------------------------
// Source spans
// ---------------------------------------------------------------------------

func TestContainerDirectiveSpanIsOpeningFenceOnly(t *testing.T) {
	src := ":::note[Label]{#id}\ncontent\n:::\n"
	root := parse(t, src)
	cd, ok := findNode(root, KindContainerDirective).(*ContainerDirective)
	if !ok {
		t.Fatal("no container directive parsed")
	}
	if got := spanText(src, cd.Span); got != ":::note[Label]{#id}" {
		t.Errorf("container span %q", got)
	}
	cf, ok := findNode(root, KindCloseFence).(*CloseFence)
	if !ok {
		t.Fatal("no close fence parsed")
	}
	if got := spanText(src, cf.Span); got != ":::" {
		t.Errorf("close fence span %q", got)
	}
	// The full extent runs from the opening fence to the matching close.
	if got := src[cd.Span.Start:cf.Span.Stop]; got != ":::note[Label]{#id}\ncontent\n:::" {
		t.Errorf("container extent %q", got)
	}
}

func TestContainerDirectiveSpanWithoutLabel(t *testing.T) {
	src := "before\n\n:::note{#id}\ncontent\n:::\n"
	cd, ok := findNode(parse(t, src), KindContainerDirective).(*ContainerDirective)
	if !ok {
		t.Fatal("no container directive parsed")
	}
	if got := spanText(src, cd.Span); got != ":::note{#id}" {
		t.Errorf("container span %q", got)
	}
}

func TestNestedContainerDirectiveSpans(t *testing.T) {
	src := "::::outer\n:::inner\nx\n:::\n::::\n"
	root := parse(t, src)
	containers := collectNodes(root, KindContainerDirective)
	if len(containers) != 2 {
		t.Fatalf("got %d containers, want 2", len(containers))
	}
	fences := collectNodes(root, KindCloseFence)
	if len(fences) != 2 {
		t.Fatalf("got %d close fences, want 2", len(fences))
	}
	outer, ok := containers[0].(*ContainerDirective)
	if !ok {
		t.Fatalf("wrong node type %T", containers[0])
	}
	inner, ok := containers[1].(*ContainerDirective)
	if !ok {
		t.Fatalf("wrong node type %T", containers[1])
	}
	innerFence, ok := fences[0].(*CloseFence)
	if !ok {
		t.Fatalf("wrong node type %T", fences[0])
	}
	outerFence, ok := fences[1].(*CloseFence)
	if !ok {
		t.Fatalf("wrong node type %T", fences[1])
	}
	if got := spanText(src, outer.Span); got != "::::outer" {
		t.Errorf("outer span %q", got)
	}
	if got := spanText(src, inner.Span); got != ":::inner" {
		t.Errorf("inner span %q", got)
	}
	if got := src[inner.Span.Start:innerFence.Span.Stop]; got != ":::inner\nx\n:::" {
		t.Errorf("inner extent %q", got)
	}
	if got := src[outer.Span.Start:outerFence.Span.Stop]; got != src[:len(src)-1] {
		t.Errorf("outer extent %q", got)
	}
}

func TestContainerDirectiveSpanIgnoresShortInnerFence(t *testing.T) {
	// The 3-colon line is too short to close the 4-colon container, so it
	// stays body content and the extent runs to the 4-colon fence.
	src := "::::note\nx\n:::\ny\n::::\n"
	root := parse(t, src)
	cd, ok := findNode(root, KindContainerDirective).(*ContainerDirective)
	if !ok {
		t.Fatal("no container directive parsed")
	}
	fences := collectNodes(root, KindCloseFence)
	if len(fences) != 1 {
		t.Fatalf("got %d close fences, want 1", len(fences))
	}
	cf, ok := fences[0].(*CloseFence)
	if !ok {
		t.Fatalf("wrong node type %T", fences[0])
	}
	if got := spanText(src, cd.Span); got != "::::note" {
		t.Errorf("container span %q", got)
	}
	if got := spanText(src, cf.Span); got != "::::" {
		t.Errorf("close fence span %q", got)
	}
	if got := src[cd.Span.Start:cf.Span.Stop]; got != src[:len(src)-1] {
		t.Errorf("container extent %q", got)
	}
}

func TestUnclosedContainerDirectiveSpan(t *testing.T) {
	// Without a closing fence there is no CloseFence node: Span still covers
	// the opening fence and consumers must take the extent to end of input.
	src := ":::note[Label]\ncontent\n"
	root := parse(t, src)
	cd, ok := findNode(root, KindContainerDirective).(*ContainerDirective)
	if !ok {
		t.Fatal("no container directive parsed")
	}
	if got := spanText(src, cd.Span); got != ":::note[Label]" {
		t.Errorf("container span %q", got)
	}
	if fences := collectNodes(root, KindCloseFence); len(fences) != 0 {
		t.Errorf("got %d close fences, want 0", len(fences))
	}
}

func TestLeafDirectiveSpan(t *testing.T) {
	src := "before\n\n::media[shot.png]{width=\"80\"}\n\nafter\n"
	ld, ok := findNode(parse(t, src), KindLeafDirective).(*LeafDirective)
	if !ok {
		t.Fatal("no leaf directive parsed")
	}
	if got := spanText(src, ld.Span); got != "::media[shot.png]{width=\"80\"}" {
		t.Errorf("leaf span %q", got)
	}
}

func TestTextDirectiveSpan(t *testing.T) {
	src := "a :status[Ready]{color=\"green\"} b\n"
	td, ok := findNode(parse(t, src), KindTextDirective).(*TextDirective)
	if !ok {
		t.Fatal("no text directive parsed")
	}
	if got := spanText(src, td.Span); got != ":status[Ready]{color=\"green\"}" {
		t.Errorf("text span %q", got)
	}
}

func TestTextDirectiveSpanBareName(t *testing.T) {
	src := "see :ref here\n"
	td, ok := findNode(parse(t, src), KindTextDirective).(*TextDirective)
	if !ok {
		t.Fatal("no text directive parsed")
	}
	if got := spanText(src, td.Span); got != ":ref" {
		t.Errorf("text span %q", got)
	}
}

func TestContainerDirectiveSpanInsideBlockquote(t *testing.T) {
	// Block markers ('> ') are consumed before the directive parser sees the
	// line, so the span still points at the fence itself.
	src := "> :::note\n> x\n> :::\n"
	root := parse(t, src)
	cd, ok := findNode(root, KindContainerDirective).(*ContainerDirective)
	if !ok {
		t.Fatal("no container directive parsed")
	}
	cf, ok := findNode(root, KindCloseFence).(*CloseFence)
	if !ok {
		t.Fatal("no close fence parsed")
	}
	if got := spanText(src, cd.Span); got != ":::note" {
		t.Errorf("container span %q", got)
	}
	if got := spanText(src, cf.Span); got != ":::" {
		t.Errorf("close fence span %q", got)
	}
}
