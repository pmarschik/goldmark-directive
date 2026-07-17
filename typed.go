package directive

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"

	"github.com/yuin/goldmark/ast"
)

// nodeOf constrains PT to *T implementing ast.Node, so typed registration
// is checked at compile time.
type nodeOf[T any] interface {
	*T
	ast.Node
}

// Container returns a handler that constructs *T for a container
// directive, binding attributes into `directive:"name"`-tagged fields
// (see BindAttrs). Use with Handlers or RegisterContainer.
func Container[T any, PT nodeOf[T]]() func(*ContainerDirective) ast.Node {
	valueFieldIndex(reflect.TypeFor[T]())
	return func(d *ContainerDirective) ast.Node {
		n := PT(new(T))
		BindAttrs(n, d.Attrs)
		return n
	}
}

// Leaf returns a handler that constructs *T for a leaf directive, binding
// attributes into `directive:"name"`-tagged fields.
func Leaf[T any, PT nodeOf[T]]() func(*LeafDirective) ast.Node {
	valueFieldIndex(reflect.TypeFor[T]())
	return func(d *LeafDirective) ast.Node {
		n := PT(new(T))
		BindAttrs(n, d.Attrs)
		return n
	}
}

// Text returns a handler that constructs *T for a text directive, binding
// attributes into `directive:"name"`-tagged fields. The label binds to a
// string field tagged `directive:",label"`.
func Text[T any, PT nodeOf[T]]() func(*TextDirective) ast.Node {
	valueFieldIndex(reflect.TypeFor[T]())
	return func(d *TextDirective) ast.Node {
		n := PT(new(T))
		BindAttrs(n, d.Attrs)
		bindLabel(n, string(d.LabelSource))
		return n
	}
}

// RegisterContainer registers a typed container directive: parsing
// :::name constructs *T with its tagged fields bound from the attributes.
func RegisterContainer[T any, PT nodeOf[T]](h *Handlers, name string) {
	if h.Container == nil {
		h.Container = map[string]func(*ContainerDirective) ast.Node{}
	}
	h.Container[name] = Container[T, PT]()
}

// RegisterLeaf registers a typed leaf directive for ::name.
func RegisterLeaf[T any, PT nodeOf[T]](h *Handlers, name string) {
	if h.Leaf == nil {
		h.Leaf = map[string]func(*LeafDirective) ast.Node{}
	}
	h.Leaf[name] = Leaf[T, PT]()
}

// RegisterText registers a typed text directive for :name.
func RegisterText[T any, PT nodeOf[T]](h *Handlers, name string) {
	if h.Text == nil {
		h.Text = map[string]func(*TextDirective) ast.Node{}
	}
	h.Text[name] = Text[T, PT]()
}

// BindAttrs populates target's `directive:"name"`-tagged fields from a
// directive's attributes. Supported field types: string, bool, all int/
// uint sizes, and floats; missing attributes leave the field's zero value,
// unparsable values are skipped. The #id and .class shorthands arrive
// under the "id" and "class" attribute names.
//
// A field tagged `directive:",value"` binds the bare-value form instead:
// when the attributes contain exactly one key with an empty value (as
// {14} or {wide} parse), that key string is converted into the field.
// With zero or multiple empty-valued attributes the field stays zero.
// A ",value" field must be the only attr-binding `directive:` tag on the
// struct (a `directive:",label"` field is still allowed — it binds the
// label, not attributes); violating this panics at registration time, or
// on first bind when BindAttrs is called directly.
func BindAttrs(target ast.Node, attrs map[string]string) {
	v := reflect.ValueOf(target).Elem()
	t := v.Type()
	if idx := valueFieldIndex(t); idx >= 0 {
		if key, ok := singleBareAttr(attrs); ok {
			setField(v.Field(idx), key)
		}
		return
	}
	if len(attrs) == 0 {
		return
	}
	for i := range t.NumField() {
		name, ok := t.Field(i).Tag.Lookup("directive")
		if !ok || name == "" || name == ",label" {
			continue
		}
		raw, found := attrs[name]
		if !found {
			continue
		}
		setField(v.Field(i), raw)
	}
}

// valueFieldCache memoizes per-type ",value" validation so the check runs
// once per type (reflect.Type -> int field index, -1 when absent).
var valueFieldCache sync.Map

// valueFieldIndex returns the index of the `directive:",value"` field of t,
// or -1 when t has none. It panics when the ",value" tag coexists with any
// other attr-binding `directive:` tag (named attrs or a second ",value");
// `directive:",label"` binds the label and is allowed alongside.
func valueFieldIndex(t reflect.Type) int {
	if cached, ok := valueFieldCache.Load(t); ok {
		if idx, isInt := cached.(int); isInt {
			return idx
		}
	}
	valueIdx := -1
	otherTag := ""
	for i := range t.NumField() {
		tag, ok := t.Field(i).Tag.Lookup("directive")
		if !ok || tag == "" || tag == ",label" {
			continue
		}
		if tag == ",value" {
			if valueIdx >= 0 {
				panic(fmt.Sprintf("directive: %s has multiple directive:\",value\" fields", t))
			}
			valueIdx = i
			continue
		}
		otherTag = tag
	}
	if valueIdx >= 0 && otherTag != "" {
		panic(fmt.Sprintf("directive: %s combines directive:\",value\" with directive:%q; \",value\" must be the only attr-binding tag", t, otherTag))
	}
	valueFieldCache.Store(t, valueIdx)
	return valueIdx
}

// singleBareAttr returns the single bare attribute key — the one key whose
// value is the empty string, as {14} or {wide} parse — and false when there
// are zero or multiple empty-valued attributes.
func singleBareAttr(attrs map[string]string) (string, bool) {
	key, found := "", false
	for k, v := range attrs {
		if v != "" {
			continue
		}
		if found {
			return "", false
		}
		key, found = k, true
	}
	return key, found
}

// bindLabel assigns the raw label text to the field tagged
// `directive:",label"`, if any.
func bindLabel(target ast.Node, label string) {
	v := reflect.ValueOf(target).Elem()
	t := v.Type()
	for i := range t.NumField() {
		if tag, ok := t.Field(i).Tag.Lookup("directive"); ok && tag == ",label" {
			f := v.Field(i)
			if f.Kind() == reflect.String && f.CanSet() {
				f.SetString(label)
			}
			return
		}
	}
}

func setField(f reflect.Value, raw string) {
	if !f.CanSet() {
		return
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(raw)
	case reflect.Bool:
		if b, err := strconv.ParseBool(raw); err == nil {
			f.SetBool(b)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && !f.OverflowInt(n) {
			f.SetInt(n)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil && !f.OverflowUint(n) {
			f.SetUint(n)
		}
	case reflect.Float32, reflect.Float64:
		if n, err := strconv.ParseFloat(raw, 64); err == nil && !f.OverflowFloat(n) {
			f.SetFloat(n)
		}
	default:
		// Unsupported field type — leave the zero value.
	}
}
