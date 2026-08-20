// Package directive implements remark-directive-compatible generic
// directives for goldmark:
//
//	container  :::name[label]{attrs} ... :::   (block, nestable)
//	leaf       ::name[label]{attrs}            (block, single line)
//	text       :name[label]{attrs}             (inline)
//
// The compatibility target is the observed behavior of remark-directive
// (micromark-extension-directive), measured against its actual output.
package directive

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Measured remark-directive rules implemented here:
//
//   - names start alphanumeric and may contain '-'/'_' but not end with them;
//     a trailing '-'/'_' invalidates the whole directive
//   - a text directive fails when preceded by ':' or '\' and when the name is
//     directly followed by ':' (protects emoji shortcodes like :smile:)
//   - any non-whitespace left on a container/leaf marker line after
//     name[label]{attrs} invalidates the directive (the line becomes text)
//   - container close fences need at least as many colons as the opening
//     fence and close the outermost matching container
//   - container labels become the first child paragraph (parsed as inlines);
//     leaf/text labels become the node's inline children

// ---------------------------------------------------------------------------
// AST nodes
// ---------------------------------------------------------------------------

// KindContainerDirective is the node kind for :::name container directives.
var KindContainerDirective = ast.NewNodeKind("ContainerDirective")

// ContainerDirective represents a :::name[label]{attrs} … ::: block. The
// label, when present, is the node's first child paragraph.
type ContainerDirective struct {
	Name  string
	Attrs map[string]string
	ast.BaseBlock
	// Span covers the OPENING FENCE LINE ONLY — it is the one directive
	// span that is not the node's whole extent, because the end is not
	// known when the block opens. For a closed container the full extent
	// runs from Span.Start to the matching CloseFence's Span.Stop; an
	// unclosed container has no CloseFence at all, so consumers must take
	// its end from the last child or the end of the enclosing block.
	// Offsets are byte offsets into the parsed source (text.Segment
	// semantics); the line terminator is excluded.
	Span text.Segment
	// fenceLength is the number of colons in the opening fence; the closing
	// fence needs at least as many.
	fenceLength int
}

func (*ContainerDirective) Kind() ast.NodeKind { return KindContainerDirective }

func (n *ContainerDirective) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Name": n.Name}, nil)
}

// KindLeafDirective is the node kind for ::name leaf directives.
var KindLeafDirective = ast.NewNodeKind("LeafDirective")

// LeafDirective represents a single-line ::name[label]{attrs} block. The
// label content is stored as the node's line and becomes its inline children.
type LeafDirective struct {
	Name  string
	Attrs map[string]string
	ast.BaseBlock
	// Span covers the directive's whole line, terminator excluded. Offsets
	// are byte offsets into the parsed source (text.Segment semantics).
	Span text.Segment
}

func (*LeafDirective) Kind() ast.NodeKind { return KindLeafDirective }

func (n *LeafDirective) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Name": n.Name}, nil)
}

// KindTextDirective is the node kind for :name inline directives.
var KindTextDirective = ast.NewNodeKind("TextDirective")

// TextDirective represents an inline :name[label]{attrs} directive. Because
// goldmark's inline pass cannot re-enter arbitrary ranges, the label is
// parsed with a nested parser over its own source slice: LabelRoot's
// segments reference LabelSource, not the outer document source.
type TextDirective struct {
	Name  string
	Attrs map[string]string
	// LabelSource is the raw label content ([…]), nil when absent.
	LabelSource []byte
	// LabelRoot is the paragraph holding the parsed label inlines (segments
	// reference LabelSource). Nil when the label is absent or not parseable
	// as inline content.
	LabelRoot ast.Node
	ast.BaseInline
	// Span covers the whole :name[label]{attrs} directive. Offsets are byte
	// offsets into the parsed source (text.Segment semantics) — unlike
	// LabelRoot's segments, which reference LabelSource.
	Span text.Segment
}

func (*TextDirective) Kind() ast.NodeKind { return KindTextDirective }

func (n *TextDirective) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Name": n.Name}, nil)
}

// ---------------------------------------------------------------------------
// Shared scanners
// ---------------------------------------------------------------------------

func isDirectiveAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// scanDirectiveName reads a directive name starting at s[i]: alphanumeric,
// optionally containing '-'/'_' runs. Returns the index after the name, or
// -1 when there is no valid name (including names ending in '-'/'_', which
// invalidate the whole directive in micromark).
func scanDirectiveName(s []byte, i int) int {
	if i >= len(s) || !isDirectiveAlnum(s[i]) {
		return -1
	}
	end := i + 1
	for end < len(s) && (isDirectiveAlnum(s[end]) || s[end] == '-' || s[end] == '_') {
		end++
	}
	if s[end-1] == '-' || s[end-1] == '_' {
		return -1
	}
	return end
}

// scanDirectiveLabel reads a [label] starting at s[i]. The label ends at the
// first ']' on the same line. Returns content start/end and the index after
// ']'; ok is false when there is no complete label.
func scanDirectiveLabel(s []byte, i int) (start, end, next int, ok bool) {
	if i >= len(s) || s[i] != '[' {
		return 0, 0, i, false
	}
	j := i + 1
	for j < len(s) && s[j] != ']' && s[j] != '\n' && s[j] != '\r' {
		j++
	}
	if j >= len(s) || s[j] != ']' {
		return 0, 0, i, false
	}
	return i + 1, j, j + 1, true
}

// scanDirectiveAttributes reads a {…} attribute block starting at s[i],
// supporting #id and .class shortcuts, bare keys, and key=value pairs with
// optional single/double quoting. Returns the parsed attributes and the
// index after '}'; ok is false when the block is absent or malformed.
func scanDirectiveAttributes(s []byte, i int) (attrs map[string]string, next int, ok bool) {
	if i >= len(s) || s[i] != '{' {
		return nil, i, false
	}
	attrs = map[string]string{}
	j := i + 1
	for {
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j >= len(s) || s[j] == '\n' || s[j] == '\r' {
			return nil, i, false
		}
		if s[j] == '}' {
			return attrs, j + 1, true
		}
		var after int
		var valid bool
		switch s[j] {
		case '#', '.':
			after, valid = scanAttrShorthand(s, j, attrs)
		default:
			after, valid = scanAttrKeyValue(s, j, attrs)
		}
		if !valid {
			return nil, i, false
		}
		j = after
	}
}

// scanAttrShorthand parses a #id or .class shortcut starting at the marker
// byte s[j] into attrs. Returns the index after the value; ok is false when
// the value is empty.
func scanAttrShorthand(s []byte, j int, attrs map[string]string) (next int, ok bool) {
	marker := s[j]
	j++
	start := j
	for j < len(s) && !isAttrBoundary(s[j]) {
		j++
	}
	if j == start {
		return 0, false
	}
	value := string(s[start:j])
	if marker == '#' {
		attrs["id"] = value
	} else if existing, found := attrs["class"]; found {
		attrs["class"] = existing + " " + value
	} else {
		attrs["class"] = value
	}
	return j, true
}

// scanAttrKeyValue parses a bare key or key=value pair starting at s[j] into
// attrs. Returns the index after the pair; ok is false when the key is empty
// or a quoted value is unterminated.
func scanAttrKeyValue(s []byte, j int, attrs map[string]string) (next int, ok bool) {
	start := j
	for j < len(s) && !isAttrBoundary(s[j]) && s[j] != '=' {
		j++
	}
	if j == start {
		return 0, false
	}
	key := string(s[start:j])
	value := ""
	if j < len(s) && s[j] == '=' {
		var valid bool
		value, j, valid = scanAttrValue(s, j+1)
		if !valid {
			return 0, false
		}
	}
	attrs[key] = value
	return j, true
}

// scanAttrValue parses an attribute value starting at s[j] (just after '='),
// with optional single/double quoting. Returns the value and the index after
// it; ok is false when a quoted value is unterminated.
func scanAttrValue(s []byte, j int) (value string, next int, ok bool) {
	if j < len(s) && (s[j] == '"' || s[j] == '\'') {
		quote := s[j]
		j++
		vStart := j
		for j < len(s) && s[j] != quote && s[j] != '\n' && s[j] != '\r' {
			j++
		}
		if j >= len(s) || s[j] != quote {
			return "", 0, false
		}
		return string(s[vStart:j]), j + 1, true
	}
	vStart := j
	for j < len(s) && !isAttrBoundary(s[j]) {
		j++
	}
	return string(s[vStart:j]), j, true
}

func isAttrBoundary(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' ||
		c == '{' || c == '}' || c == '"' || c == '\''
}

// directiveMarker is the parsed form of a container/leaf marker line.
type directiveMarker struct {
	attrs      map[string]string
	name       string
	colons     int
	labelStart int // offsets into the line; valid when hasLabel
	labelEnd   int
	hasLabel   bool
}

// scanDirectiveMarkerLine parses a full container/leaf marker line
// (":::name[label]{attrs}" with up to 3 leading spaces). Any trailing
// non-whitespace invalidates the marker, matching remark.
func scanDirectiveMarkerLine(line []byte) *directiveMarker {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	colons := 0
	for i < len(line) && line[i] == ':' {
		colons++
		i++
	}
	if colons < 2 {
		return nil
	}
	nameEnd := scanDirectiveName(line, i)
	if nameEnd < 0 {
		return nil
	}
	m := &directiveMarker{colons: colons, name: string(line[i:nameEnd])}
	i = nameEnd
	if start, end, next, ok := scanDirectiveLabel(line, i); ok {
		m.hasLabel = true
		m.labelStart, m.labelEnd = start, end
		i = next
	}
	if attrs, next, ok := scanDirectiveAttributes(line, i); ok {
		m.attrs = attrs
		i = next
	}
	for i < len(line) {
		if line[i] != ' ' && line[i] != '\t' && line[i] != '\n' && line[i] != '\r' {
			return nil
		}
		i++
	}
	return m
}

// lineSpan returns the source span of the line segment seg with its
// terminator stripped. It works off the source rather than the peeked line
// so virtual bytes (segment padding, forced newlines) never shift the
// offsets, which are byte offsets into source.
func lineSpan(source []byte, seg text.Segment) text.Segment {
	stop := seg.Stop
	for stop > seg.Start && (source[stop-1] == '\n' || source[stop-1] == '\r') {
		stop--
	}
	return text.NewSegment(seg.Start, stop)
}

// isCloseFence reports whether line is a valid closing fence for a
// container opened with fenceLength colons (≥ fenceLength colons, up to 3
// leading spaces, nothing but whitespace after).
func isCloseFence(line []byte, fenceLength int) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	colons := 0
	for i < len(line) && line[i] == ':' {
		colons++
		i++
	}
	if colons < fenceLength {
		return false
	}
	rest := bytes.TrimRight(line[i:], " \t\n\r")
	return len(rest) == 0
}

// ---------------------------------------------------------------------------
// Container directive block parser
// ---------------------------------------------------------------------------

type containerDirectiveParser struct{ cfg *parserConfig }

// NewDirectiveParser returns a goldmark block parser for :::name container
// directives.
func NewDirectiveParser(opts ...Option) parser.BlockParser {
	return &containerDirectiveParser{cfg: applyOptions(opts)}
}

func (*containerDirectiveParser) Trigger() []byte { return []byte{':'} }

func (p *containerDirectiveParser) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	m := scanDirectiveMarkerLine(line)
	if m == nil || m.colons < 3 || !p.cfg.accepts(m.name) {
		return nil, parser.NoChildren
	}

	node := &ContainerDirective{
		Name:        m.name,
		Attrs:       m.attrs,
		Span:        lineSpan(reader.Source(), seg),
		fenceLength: m.colons,
	}
	// The label becomes the first child paragraph; its line segment points
	// into the outer source so the regular inline pass parses it.
	if m.hasLabel && m.labelEnd > m.labelStart {
		labelPara := ast.NewParagraph()
		labelPara.SetAttributeString("directiveLabel", true)
		labelPara.Lines().Append(text.NewSegment(seg.Start+m.labelStart, seg.Start+m.labelEnd))
		node.AppendChild(node, labelPara)
	}

	reader.Advance(len(bytes.TrimRight(line, "\n\r")))
	return node, parser.HasChildren
}

func (*containerDirectiveParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, _ := reader.PeekLine()
	cd, ok := node.(*ContainerDirective)
	if ok && isCloseFence(line, cd.fenceLength) {
		// Detect the close but do NOT consume the line: goldmark re-evaluates
		// the line after Close, and an open inner paragraph would otherwise
		// lazily continue across the fence, canceling the close. The
		// closeFenceParser (which can interrupt paragraphs) consumes the
		// fence line instead.
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}

func (*containerDirectiveParser) Close(_ ast.Node, _ text.Reader, _ parser.Context) {}

func (*containerDirectiveParser) CanInterruptParagraph() bool { return true }

func (*containerDirectiveParser) CanAcceptIndentedLine() bool { return false }

// ---------------------------------------------------------------------------
// Close fence parser
// ---------------------------------------------------------------------------

// KindCloseFence is the node kind for consumed closing fences.
var KindCloseFence = ast.NewNodeKind("DirectiveCloseFence")

// CloseFence is an inert marker for a consumed ::: closing fence.
// It exists only so the fence line can interrupt an open paragraph via the
// regular block-open machinery; consumers must ignore it.
type CloseFence struct {
	ast.BaseBlock
	// Span covers the fence line, terminator excluded. Offsets are byte
	// offsets into the parsed source (text.Segment semantics); the matching
	// ContainerDirective's full extent ends at Span.Stop.
	Span text.Segment
}

func (*CloseFence) Kind() ast.NodeKind { return KindCloseFence }

func (n *CloseFence) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type closeFenceParser struct{}

// NewCloseFenceParser returns the block parser that consumes container
// directive closing fences. Container directives signal Close without
// consuming the fence line (see containerDirectiveParser.Continue); this
// parser then opens on that line — interrupting any open paragraph, which a
// bare ::: line cannot do on its own — and swallows it as an inert marker.
func NewCloseFenceParser() parser.BlockParser {
	return &closeFenceParser{}
}

func (*closeFenceParser) Trigger() []byte { return []byte{':'} }

func (*closeFenceParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	// Only bare fences: an open container directive among the currently open
	// blocks must accept this line as its closing fence.
	matched := false
	for _, be := range pc.OpenedBlocks() {
		if cd, ok := be.Node.(*ContainerDirective); ok && isCloseFence(line, cd.fenceLength) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, parser.NoChildren
	}
	span := lineSpan(reader.Source(), seg)
	reader.Advance(seg.Len() - 1)
	return &CloseFence{Span: span}, parser.NoChildren
}

func (*closeFenceParser) Continue(_ ast.Node, _ text.Reader, _ parser.Context) parser.State {
	return parser.Close
}

func (*closeFenceParser) Close(_ ast.Node, _ text.Reader, _ parser.Context) {}

func (*closeFenceParser) CanInterruptParagraph() bool { return true }

func (*closeFenceParser) CanAcceptIndentedLine() bool { return false }

// ---------------------------------------------------------------------------
// Leaf directive block parser
// ---------------------------------------------------------------------------

type leafDirectiveParser struct{ cfg *parserConfig }

// NewLeafDirectiveParser returns a goldmark block parser for single-line
// ::name leaf directives.
func NewLeafDirectiveParser(opts ...Option) parser.BlockParser {
	return &leafDirectiveParser{cfg: applyOptions(opts)}
}

func (*leafDirectiveParser) Trigger() []byte { return []byte{':'} }

func (p *leafDirectiveParser) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	m := scanDirectiveMarkerLine(line)
	if m == nil || m.colons != 2 || !p.cfg.accepts(m.name) {
		return nil, parser.NoChildren
	}

	node := &LeafDirective{Name: m.name, Attrs: m.attrs, Span: lineSpan(reader.Source(), seg)}
	// The label becomes the node's line so the inline pass parses it into
	// the node's children (like an ATX heading's text).
	if m.hasLabel && m.labelEnd > m.labelStart {
		node.Lines().Append(text.NewSegment(seg.Start+m.labelStart, seg.Start+m.labelEnd))
	}

	reader.Advance(len(bytes.TrimRight(line, "\n\r")))
	return node, parser.NoChildren
}

func (*leafDirectiveParser) Continue(_ ast.Node, _ text.Reader, _ parser.Context) parser.State {
	return parser.Close
}

func (*leafDirectiveParser) Close(_ ast.Node, _ text.Reader, _ parser.Context) {}

func (*leafDirectiveParser) CanInterruptParagraph() bool { return true }

func (*leafDirectiveParser) CanAcceptIndentedLine() bool { return false }

// ---------------------------------------------------------------------------
// Text directive inline parser
// ---------------------------------------------------------------------------

type textDirectiveParser struct {
	labelParser func() parser.Parser
	cfg         *parserConfig
}

// NewTextDirectiveParser returns a goldmark inline parser for :name text
// directives. Labels are parsed with a nested parser built by labelParser —
// pass the factory for your document parser so label inlines support the
// same constructs; nil falls back to a minimal parser with the directive
// extension registered.
func NewTextDirectiveParser(labelParser func() parser.Parser, opts ...Option) parser.InlineParser {
	if labelParser == nil {
		labelParser = defaultLabelParser
	}
	return &textDirectiveParser{labelParser: labelParser, cfg: applyOptions(opts)}
}

// defaultLabelParser builds the fallback label parser: goldmark defaults
// plus this package's directive parsers.
func defaultLabelParser() parser.Parser {
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

func (*textDirectiveParser) Trigger() []byte { return []byte{':'} }

func (p *textDirectiveParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 2 || line[0] != ':' {
		return nil
	}
	// A text directive cannot start right after another ':' (":::" runs in
	// prose stay literal) or after a backslash escape.
	prev := block.PrecendingCharacter()
	if prev == ':' {
		return nil
	}
	// A '\\' before the ':' may be the escape marker OR an escaped literal
	// backslash that escapes nothing — only the run's parity says which,
	// and micromark decides it the same way.
	if prev == '\\' && precedingBackslashRunIsOdd(block.Source(), seg.Start) {
		return nil
	}
	nameEnd := scanDirectiveName(line, 1)
	if nameEnd < 0 {
		return nil
	}
	// A name directly followed by ':' is not a directive — this protects
	// emoji shortcodes like :smile:.
	if nameEnd < len(line) && line[nameEnd] == ':' {
		return nil
	}
	name := string(line[1:nameEnd])
	if !p.cfg.accepts(name) {
		return nil
	}
	consumed := nameEnd

	node := &TextDirective{Name: name}
	if start, end, next, ok := scanDirectiveLabel(line, consumed); ok {
		consumed = next
		if end > start {
			node.LabelSource = bytes.Clone(line[start:end])
			node.LabelRoot = parseInlineFragment(p.labelParser(), node.LabelSource)
		}
	}
	if attrs, next, ok := scanDirectiveAttributes(line, consumed); ok {
		node.Attrs = attrs
		consumed = next
	}

	// seg.Start is the source offset of the directive's ':' — the trigger
	// only fires on line[0], so no segment padding sits in front of it.
	node.Span = text.NewSegment(seg.Start, seg.Start+consumed)

	block.Advance(consumed)
	return node
}

// precedingBackslashRunIsOdd reports whether the run of backslashes ending
// at src[pos-1] has odd length, i.e. whether the last one is still acting
// as an escape marker.
//
// PrecendingCharacter only sees ONE byte back, which cannot tell "\:u" (the
// colon is escaped, no directive) from "\\:u" (the pair is an escaped
// literal backslash and the colon is free). Every even-length run pairs up
// into literal backslashes; only an odd one leaves a marker over. Reading
// the raw source rather than the decoded text is safe for this: a run is
// contiguous by definition, and any byte that ends it — a newline, a list
// indent, a blockquote marker — ends it correctly at zero.
func precedingBackslashRunIsOdd(src []byte, pos int) bool {
	run := 0
	for i := pos - 1; i >= 0 && src[i] == '\\'; i-- {
		run++
	}
	return run%2 == 1
}

// parseInlineFragment parses a label fragment with a nested parser and
// returns the paragraph holding its inline children. The returned node's
// segments reference the fragment source passed in, not the outer document.
// Returns nil when the fragment does not parse to a single inline run (e.g.
// "[- list]"), in which case callers should fall back to the raw text.
func parseInlineFragment(labelParser parser.Parser, src []byte) ast.Node {
	root := labelParser.Parse(text.NewReader(src))
	first := root.FirstChild()
	if first == nil {
		return nil
	}
	switch first.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return first
	}
	return nil
}
