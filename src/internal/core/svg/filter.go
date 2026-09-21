package svg

import (
	"fmt"
	"sort"
	"strings"
)

// PolicyViolationError reports a document that broke the policy, for strict mode.
type PolicyViolationError struct{ Violations []string }

func (e *PolicyViolationError) Error() string {
	shown := e.Violations
	// The first twenty only. A corpus-wide run can produce thousands, and an
	// error message that scrolls past the terminal buffer tells you less than
	// one that fits on a screen.
	if len(shown) > 20 {
		shown = shown[:20]
	}
	return fmt.Sprintf("%d file(s) violate the SVG policy:\n  %s",
		len(e.Violations), strings.Join(shown, "\n  "))
}

// SanitiseBytes filters a document to what the policy permits.
//
// Returns the serialised result and the names of everything removed, so a
// caller can report *what* was wrong rather than only that something was.
func SanitiseBytes(raw []byte, alsoStripClass bool) ([]byte, []string, error) {
	p, err := Load()
	if err != nil {
		return nil, nil, err
	}

	root, err := Parse(raw, p.StripComments)
	if err != nil {
		return nil, nil, err
	}

	var removed []string
	filterElement(p, root, &removed, alsoStripClass)

	return Serialise(root), removed, nil
}

// filterElement removes disallowed children, then filters this element's own
// attributes.
//
// Children first, then attributes -- and the root element's own *tag* is never
// checked, only its attributes. That is what the Python does, and it matters:
// checking the root would delete the <svg> element itself and leave nothing to
// serialise.
func filterElement(p *Policy, el *Node, removed *[]string, alsoStripClass bool) {
	kept := el.Children[:0]
	for i := 0; i < len(el.Children); i++ {
		child := el.Children[i]

		if child.Kind != ElementNode {
			kept = append(kept, child)
			continue
		}

		if !p.TagAllowed(child.Local) {
			*removed = append(*removed, "<"+child.Local+">")

			// Drop the text that followed it too. lxml models trailing text as
			// the element's own .tail, so el.remove(child) takes both; a tree
			// built from Go's decoder holds it as a separate sibling node, and
			// leaving it behind means every removed element deposits its
			// indentation in the output.
			//
			// Found by diffing a real sync against what a previous Python run
			// committed -- 538 of flag's 542 icons matched byte for byte and
			// four did not, each by exactly one orphaned newline-and-indent.
			// The corpus differential could not see it: that compares surviving
			// structure, and whitespace is not structure.
			if i+1 < len(el.Children) && el.Children[i+1].Kind == TextNode {
				i++
			}
			continue
		}

		filterElement(p, child, removed, alsoStripClass)
		kept = append(kept, child)
	}
	el.Children = kept

	attrs := el.Attrs[:0]
	for _, a := range el.Attrs {
		// Namespace declarations are not attributes as far as the policy is
		// concerned; lxml never surfaces them at all. See Attr.IsNamespaceDecl.
		if a.IsNamespaceDecl {
			attrs = append(attrs, a)
			continue
		}

		name := a.Name()
		if !p.AttributeAllowed(name, a.Value, alsoStripClass) {
			*removed = append(*removed, name)
			continue
		}
		attrs = append(attrs, a)
	}
	el.Attrs = attrs
}

// SummariseRemoved collapses a removal list into one sorted, deduplicated line,
// which is what the per-file violation report shows.
func SummariseRemoved(removed []string) string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(removed))
	for _, r := range removed {
		if !seen[r] {
			seen[r] = true
			unique = append(unique, r)
		}
	}
	sort.Strings(unique)
	return strings.Join(unique, ", ")
}
