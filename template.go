package vue

import (
	"bytes"
	"fmt"
	"github.com/cbroglie/mustache"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"io"
	"reflect"
	"strings"
)

const (
	v      = "v-"
	vBind  = "v-bind"
	vFor   = "v-for"
	vHtml  = "v-html"
	vIf    = "v-if"
	vModel = "v-model"
	vOn    = "v-on"
)

var attrOrder = []string{vFor, vIf, vModel, vOn, vBind, vHtml}

type template struct {
	comp *Comp
	id   int64
}

// newTemplate creates a new template.
func newTemplate(comp *Comp) *template {
	return &template{comp: comp}
}

// execute executes the template with the given data to be rendered.
func (tmpl *template) execute(data map[string]interface{}) *html.Node {
	node := parseNode(tmpl.comp.tmpl)

	tmpl.executeElement(node, data)
	executeText(node, data)

	return node
}

// executeElement recursively traverses the html node and templates the elements.
// The next node is always returned which allows execution to jump around as needed.
func (tmpl *template) executeElement(node *html.Node, data map[string]interface{}) *html.Node {
	// Leave the text nodes to be executed.
	if node.Type != html.ElementNode {
		return node.NextSibling
	}

	// Attempt to create a subcomponent from the element.
	sub, ok := tmpl.comp.newSub(node.Data)

	// Order attributes before execution.
	orderAttrs(node)

	// Execute attributes.
	for i := 0; i < len(node.Attr); i++ {
		attr := node.Attr[i]
		if strings.HasPrefix(attr.Key, v) {
			deleteAttr(node, i)
			i--
			next, modified := tmpl.executeAttr(node, sub, attr, data)
			// The current node is not longer valid in favor of the next node.
			if modified {
				return next
			}
		}
	}

	// Execute subcomponent.
	if ok {
		vm := newViewModel(sub)
		subNode := vm.executeSub()
		children := children(subNode)
		for _, child := range children {
			subNode.RemoveChild(child)
			node.Parent.InsertBefore(child, node)
		}
		next := node.NextSibling
		node.Parent.RemoveChild(node)
		return next
	}

	// Execute children.
	for child := node.FirstChild; child != nil; {
		child = tmpl.executeElement(child, data)
	}

	return node.NextSibling
}

// executeText recursively executes the text node.
func executeText(node *html.Node, data map[string]interface{}) {
	switch node.Type {
	case html.TextNode:
		if strings.TrimSpace(node.Data) == "" {
			return
		}

		var err error
		node.Data, err = mustache.Render(node.Data, data)
		must(err)
	case html.ElementNode:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			executeText(child, data)
		}
	}
}

// executeAttr executes the given vue attribute.
// The next node will be executed next if the html was modified unless it is nil.
func (tmpl *template) executeAttr(node *html.Node, sub *Comp, attr html.Attribute, data map[string]interface{}) (*html.Node, bool) {
	vals := strings.Split(attr.Key, ":")
	typ, part := vals[0], ""
	if len(vals) > 1 {
		part = vals[1]
	}
	var next *html.Node
	var modified bool
	switch typ {
	case vBind:
		executeAttrBind(node, sub, part, attr.Val, data)
	case vFor:
		next, modified = tmpl.executeAttrFor(node, attr.Val, data)
	case vHtml:
		executeAttrHtml(node, attr.Val, data)
	case vIf:
		next, modified = tmpl.executeAttrIf(node, attr.Val, data)
	case vModel:
		tmpl.executeAttrModel(node, attr.Val, data)
	case vOn:
		tmpl.executeAttrOn(node, part, attr.Val)
	default:
		must(fmt.Errorf("unknown vue attribute: %v", typ))
	}
	return next, modified
}

// executeAttrBind executes the vue bind attribute.
func executeAttrBind(node *html.Node, sub *Comp, key, value string, data map[string]interface{}) {
	field, ok := data[value]
	if !ok {
		must(fmt.Errorf("unknown data field: %s", value))
	}

	prop := strings.Title(key)
	if sub.hasProp(prop) {
		sub.props[prop] = field
		return
	}

	// Remove attribute if bound to a false value of type bool.
	if val, ok := field.(bool); ok && !val {
		return
	}

	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: fmt.Sprintf("%v", field)})
}

// executeAttrFor executes the vue for attribute.
func (tmpl *template) executeAttrFor(node *html.Node, value string, data map[string]interface{}) (*html.Node, bool) {
	vals := strings.Split(value, "in")
	name := bytes.TrimSpace([]byte(vals[0]))
	field := strings.TrimSpace(vals[1])

	slice, ok := data[field]
	if !ok {
		must(fmt.Errorf("slice not found for field: %s", field))
	}

	elem := bytes.NewBuffer(nil)
	err := html.Render(elem, node)
	must(err)

	buf := bytes.NewBuffer(nil)
	values := reflect.ValueOf(slice)
	n := values.Len()
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("%s%d", name, tmpl.id)
		tmpl.id++

		b := bytes.Replace(elem.Bytes(), name, []byte(key), -1)
		_, err := buf.Write(b)
		must(err)

		data[key] = values.Index(i).Interface()
	}

	nodes := parseNodes(buf)
	for _, child := range nodes {
		node.Parent.InsertBefore(child, node)
	}
	node.Parent.RemoveChild(node)
	// The first child is the next node to execute.
	return nodes[0], true
}

// executeAttrHtml executes the vue html attribute.
func executeAttrHtml(node *html.Node, field string, data map[string]interface{}) {
	value, ok := data[field]
	if !ok {
		must(fmt.Errorf("unknown data field: %s", field))
	}
	html, ok := value.(string)
	if !ok {
		must(fmt.Errorf("data field is not of type string: %T", field))
	}

	nodes := parseNodes(strings.NewReader(html))
	for _, child := range nodes {
		node.AppendChild(child)
	}
}

// executeAttrIf executes the vue if attribute.
func (tmpl *template) executeAttrIf(node *html.Node, field string, data map[string]interface{}) (*html.Node, bool) {
	if value, ok := data[field]; ok {
		if val, ok := value.(bool); ok && val {
			return nil, false
		}
	}
	next := node.NextSibling
	node.Parent.RemoveChild(node)
	return next, true
}

// executeAttrModel executes the vue model attribute.
func (tmpl *template) executeAttrModel(node *html.Node, field string, data map[string]interface{}) {
	typ := "input"
	node.Attr = append(node.Attr, html.Attribute{Key: typ, Val: field})
	tmpl.comp.callback.addEventListener(typ, tmpl.comp.callback.vModel)

	value, ok := data[field]
	if !ok {
		must(fmt.Errorf("unknown data field: %s", field))
	}
	val, ok := value.(string)
	if !ok {
		must(fmt.Errorf("data field is not of type string: %T", field))
	}
	node.Attr = append(node.Attr, html.Attribute{Key: "value", Val: val})
}

// executeAttrOn executes the vue on attribute.
func (tmpl *template) executeAttrOn(node *html.Node, typ, method string) {
	node.Attr = append(node.Attr, html.Attribute{Key: typ, Val: method})
	tmpl.comp.callback.addEventListener(typ, tmpl.comp.callback.vOn)
}

// parseNode parses the template into an html node.
// The node returned is a placeholder, not to be rendered.
func parseNode(tmpl string) *html.Node {
	nodes := parseNodes(strings.NewReader(tmpl))
	node := &html.Node{Type: html.ElementNode}
	for _, child := range nodes {
		node.AppendChild(child)
	}
	return node
}

// parseNodes parses the reader into html nodes.
func parseNodes(reader io.Reader) []*html.Node {
	nodes, err := html.ParseFragment(reader, &html.Node{
		Type:     html.ElementNode,
		Data:     "div",
		DataAtom: atom.Div,
	})
	must(err)
	return nodes
}

// orderAttrs orders the attributes of the node which orders the template execution.
func orderAttrs(node *html.Node) {
	n := len(node.Attr)
	if n == 0 {
		return
	}
	attrs := make([]html.Attribute, 0, n)
	for _, prefix := range attrOrder {
		for _, attr := range node.Attr {
			if strings.HasPrefix(attr.Key, prefix) {
				attrs = append(attrs, attr)
			}
		}
	}
	// Append other attributes which are not vue attributes.
	for _, attr := range node.Attr {
		if !strings.HasPrefix(attr.Key, v) {
			attrs = append(attrs, attr)
		}
	}
	node.Attr = attrs
}

// deleteAttr deletes the attribute of the node at the index.
// Attribute order is preserved.
func deleteAttr(node *html.Node, i int) {
	node.Attr = append(node.Attr[:i], node.Attr[i+1:]...)
}

// children makes a slice of child html nodes.
func children(node *html.Node) []*html.Node {
	children := make([]*html.Node, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		children = append(children, child)
	}
	return children
}
