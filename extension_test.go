package directive

import (
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
)

func TestExtension_OneCallSetup(t *testing.T) {
	var h Handlers
	RegisterContainer[typedCallout](&h, "callout")

	md := goldmark.New(goldmark.WithExtensions(&Extension{
		Handlers:     h,
		AllowedNames: []string{"callout", "mention"},
	}))
	src := []byte(":::callout{level=\"info\"}\nx\n:::\n\n:::other\ny\n:::\n\na :mention[p] b\n")
	root := md.Parser().Parse(text.NewReader(src))

	if findNode(root, kindTypedCallout) == nil {
		t.Error("typed callout not parsed via Extension")
	}
	if findNode(root, KindContainerDirective) != nil {
		t.Error("unlisted container must stay text")
	}
	if findNode(root, KindTextDirective) == nil {
		t.Error("allowed text directive must parse")
	}
}

func TestExtension_RestrictToHandlers(t *testing.T) {
	var h Handlers
	RegisterContainer[typedCallout](&h, "callout")
	RegisterText[typedMention](&h, "mention")

	md := goldmark.New(goldmark.WithExtensions(&Extension{
		Handlers:           h,
		RestrictToHandlers: true,
	}))
	src := []byte(":::callout\nx\n:::\n\n:::other\ny\n:::\n\na :mention[p]{id=\"1\"} b :other[x] c\n\n::media[y]\n")
	root := md.Parser().Parse(text.NewReader(src))

	if findNode(root, kindTypedCallout) == nil {
		t.Error("registered container must parse and type")
	}
	if findNode(root, KindContainerDirective) != nil {
		t.Error("unregistered container must stay text")
	}
	if findNode(root, kindTypedMention) == nil {
		t.Error("registered text directive must parse and type")
	}
	if findNode(root, KindTextDirective) != nil {
		t.Error("unregistered text directive must stay text")
	}
	if findNode(root, KindLeafDirective) != nil {
		t.Error("leaf with no registrations must stay text")
	}
}
