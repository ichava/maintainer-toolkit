package svg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// A minimal DOM, because the policy is a tree operation and Go's encoding/xml
// is a streaming decoder.
//
// Writing one is cheaper than it looks and buys the thing that matters: an
// encoder we control. Round-tripping through xml.Encoder rewrites namespace
// prefixes -- xlink:href comes back as something like href with a generated
// prefix -- and the policy names that attribute `xlink:href`, so the rewrite
// would silently change which rule applies to it.

// Node is an element, text or comment.
type Node struct {
	Kind NodeKind

	// Element fields.
	Space, Local string
	Attrs        []Attr
	Children     []*Node

	// Text and Comment payload, unescaped.
	Text string
}

// NodeKind distinguishes the three node types this package keeps.
type NodeKind int

const (
	ElementNode NodeKind = iota
	TextNode
	CommentNode
)

// Attr is one attribute, keeping both the resolved namespace and the prefix it
// was written with.
type Attr struct {
	Space, Local string
	Value        string

	// Prefix is the source spelling, so serialisation reproduces it rather
	// than inventing one.
	Prefix string

	// IsNamespaceDecl marks xmlns and xmlns:* .
	//
	// This is the difference that would have broken every icon. lxml keeps
	// namespace declarations out of el.attrib entirely, so the Python filter
	// never sees them and they survive serialisation untouched. Go's decoder
	// hands them over as ordinary attributes -- and `xmlns` is not in
	// allowedAttributes, so filtering them would strip the SVG namespace from
	// every document. They are marked here and skipped by the filter.
	IsNamespaceDecl bool
}

// Name is the policy's spelling of the attribute.
func (a Attr) Name() string { return AttributeName(a.Space, a.Local) }

// Parse reads a document and returns its root element.
//
// Go's encoding/xml never resolves external entities and never fetches a DTD,
// so XXE and billion-laughs are closed by construction rather than by
// configuration -- the flags lxml needs (resolve_entities=False,
// no_network=True, huge_tree=False) have no Go equivalent because there is
// nothing to switch off.
//
// An unparseable document is an error, not a recovery. That is deliberate and
// differs from the runtime parser in ichava/core, which recovers from six
// common author mistakes: this gate runs before a file is committed, where the
// right answer to malformed input is to say so rather than guess.
func Parse(raw []byte, stripComments bool) (*Node, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.CharsetReader = charsetReader

	var root *Node
	stack := []*Node{}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing svg: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			node := &Node{Kind: ElementNode, Space: t.Name.Space, Local: t.Name.Local}
			for _, a := range t.Attr {
				node.Attrs = append(node.Attrs, toAttr(a))
			}
			if len(stack) == 0 {
				if root != nil {
					// Content after the root element. lxml rejects this too.
					return nil, fmt.Errorf("parsing svg: more than one root element")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("parsing svg: unexpected closing tag %q", t.Name.Local)
			}
			stack = stack[:len(stack)-1]

		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Children = append(stack[len(stack)-1].Children,
					&Node{Kind: TextNode, Text: string(t)})
			}

		case xml.Comment:
			if !stripComments && len(stack) > 0 {
				stack[len(stack)-1].Children = append(stack[len(stack)-1].Children,
					&Node{Kind: CommentNode, Text: string(t)})
			}

			// ProcInst and Directive -- the XML declaration and any DOCTYPE --
			// are dropped. Python serialises with xml_declaration=False and
			// tostring() of the root element, which emits neither.
		}
	}

	if root == nil {
		return nil, fmt.Errorf("parsing svg: no root element")
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("parsing svg: unclosed element %q", stack[len(stack)-1].Local)
	}
	return root, nil
}

// charsetReader decodes the non-UTF-8 encodings this corpus actually contains.
//
// Go's decoder refuses any declared encoding it cannot handle, and has no
// built-in table; lxml has them all. Measured across the four vendored packs,
// exactly one non-UTF-8 encoding appears -- `iso-8859-1`, on 261 files, all of
// them metronic flags -- so that is what this supports.
//
// Anything else is an error rather than a guess. Silently treating an unknown
// encoding as Latin-1 would mangle multi-byte characters into pairs of
// accented ones, and the damage would be invisible until someone looked at a
// rendered title.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return input, nil
	case "iso-8859-1", "iso8859-1", "latin-1", "latin1", "iso_8859-1":
		return latin1Reader{input}, nil
	}
	return nil, fmt.Errorf("unsupported declared encoding %q", charset)
}

// latin1Reader converts ISO-8859-1 to UTF-8. Every byte is its own code point
// in Latin-1, so the mapping needs no table.
type latin1Reader struct{ r io.Reader }

func (l latin1Reader) Read(p []byte) (int, error) {
	// Read at most half the buffer: a Latin-1 byte above 0x7F becomes two
	// UTF-8 bytes, so half the input can never overflow the output.
	limit := len(p) / 2
	if limit == 0 {
		limit = 1
	}
	buf := make([]byte, limit)

	n, err := l.r.Read(buf)
	written := 0
	for _, b := range buf[:n] {
		written += utf8.EncodeRune(p[written:], rune(b))
	}
	return written, err
}

func toAttr(a xml.Attr) Attr {
	// Go reports `xmlns="..."` as {Space:"", Local:"xmlns"} and
	// `xmlns:xlink="..."` as {Space:"xmlns", Local:"xlink"}.
	switch {
	case a.Name.Space == "xmlns":
		return Attr{Space: "xmlns", Local: a.Name.Local, Value: a.Value,
			Prefix: "xmlns", IsNamespaceDecl: true}
	case a.Name.Space == "" && a.Name.Local == "xmlns":
		return Attr{Local: "xmlns", Value: a.Value, IsNamespaceDecl: true}
	}
	return Attr{Space: a.Name.Space, Local: a.Name.Local, Value: a.Value}
}

// Serialise renders the tree.
//
// Prefixes are resolved from the namespace declarations the document itself
// carries, so a file written with xlink: comes back with xlink: rather than a
// generated prefix.
func Serialise(root *Node) []byte {
	prefixes := map[string]string{NamespaceXLink: "xlink"}
	var defaultNS string
	collectPrefixes(root, prefixes, &defaultNS)

	var b bytes.Buffer
	writeNode(&b, root, prefixes, defaultNS)
	return b.Bytes()
}

func collectPrefixes(n *Node, prefixes map[string]string, defaultNS *string) {
	if n.Kind == ElementNode {
		for _, a := range n.Attrs {
			if !a.IsNamespaceDecl {
				continue
			}
			if a.Prefix == "xmlns" {
				prefixes[a.Value] = a.Local
			} else if *defaultNS == "" {
				*defaultNS = a.Value
			}
		}
		for _, c := range n.Children {
			collectPrefixes(c, prefixes, defaultNS)
		}
	}
}

func writeNode(b *bytes.Buffer, n *Node, prefixes map[string]string, defaultNS string) {
	switch n.Kind {
	case TextNode:
		b.WriteString(escapeText(n.Text))
		return
	case CommentNode:
		b.WriteString("<!--" + n.Text + "-->")
		return
	}

	name := qualify(n.Space, n.Local, prefixes, defaultNS)
	b.WriteString("<" + name)

	for _, a := range n.Attrs {
		b.WriteString(" " + attrName(a, prefixes) + `="` + escapeAttr(a.Value) + `"`)
	}

	if len(n.Children) == 0 {
		b.WriteString("/>")
		return
	}

	b.WriteString(">")
	for _, c := range n.Children {
		writeNode(b, c, prefixes, defaultNS)
	}
	b.WriteString("</" + name + ">")
}

func attrName(a Attr, prefixes map[string]string) string {
	if a.IsNamespaceDecl {
		if a.Prefix == "xmlns" {
			return "xmlns:" + a.Local
		}
		return "xmlns"
	}
	if a.Space == "" {
		return a.Local
	}
	if prefix, ok := prefixes[a.Space]; ok {
		return prefix + ":" + a.Local
	}
	return a.Local
}

func qualify(space, local string, prefixes map[string]string, defaultNS string) string {
	if space == "" || space == defaultNS || space == NamespaceSVG {
		return local
	}
	if prefix, ok := prefixes[space]; ok {
		return prefix + ":" + local
	}
	return local
}

// escapeText and escapeAttr escape the minimum, matching what an XML
// serialiser must and no more -- over-escaping would change path data.
func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// Inventory lists every element path and attribute the document holds.
//
// It exists for the differential against the Python: comparing serialised bytes
// would fail on trivia -- attribute quoting, self-closing style, whitespace --
// that no consumer can observe, while this compares the only thing the policy
// actually decides, which is what survives.
func Inventory(root *Node) []string {
	var items []string
	var walk func(n *Node, path string)
	walk = func(n *Node, path string) {
		if n.Kind != ElementNode {
			return
		}
		here := path + "/" + n.Local
		items = append(items, "e "+here)
		for _, a := range n.Attrs {
			if a.IsNamespaceDecl {
				continue
			}
			items = append(items, "a "+here+"@"+a.Name())
		}
		for _, c := range n.Children {
			walk(c, here)
		}
	}
	walk(root, "")
	sort.Strings(items)
	return items
}
