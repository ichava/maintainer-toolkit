// Package svg enforces the shared SVG policy.
//
// svg-policy.json beside this file is a byte-identical copy of
// core/resources/security/svg-policy.json. It is vendored because this package
// has no dependency on ichava/core and cannot reach it; the copy is kept honest
// by .scripts/sync-svg-policy.mjs, which lists it as a consumer, and pinned by
// digest in the tests here.
//
// Do not hand-edit the JSON. Edit the canonical file, run the sync script with
// --write, and update the pinned digest.
package svg

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Namespace URIs the policy cares about.
const (
	NamespaceSVG   = "http://www.w3.org/2000/svg"
	NamespaceXLink = "http://www.w3.org/1999/xlink"
)

//go:embed svg-policy.json
var policyJSON []byte

// Policy is the parsed policy document.
type Policy struct {
	AllowedTags            []string         `json:"allowedTags"`
	AllowedAttributes      []string         `json:"allowedAttributes"`
	AllowedAttributePrefix []string         `json:"allowedAttributePrefixes"`
	DenyAttributePrefixes  []string         `json:"denyAttributePrefixes"`
	ForbiddenTags          []string         `json:"forbiddenTags"`
	FragmentOnlyRefs       FragmentOnlyRefs `json:"fragmentOnlyRefs"`
	StripComments          bool             `json:"stripComments"`
	StripDoctype           bool             `json:"stripDoctype"`
	StripEntities          bool             `json:"stripEntities"`
	BlockImageNonFragment  bool             `json:"blockImageNonFragmentHref"`
	BlockAnchorNonFragment bool             `json:"blockAnchorNonFragmentHref"`
	BlockCDATAScript       bool             `json:"blockCdataScript"`
	Version                int              `json:"version"`

	// StyleAttribute is NOT a boolean in the policy -- it is an object
	// describing what a style value may contain. Python reads it with a
	// truthiness test, so any non-empty object enables the `style` name.
	//
	// Typing this as `bool` is a live trap, not a tidy-up: the unmarshal fails,
	// the whole policy fails to load, and if that failure were swallowed the
	// merge below would drop `style` -- the sole paint source for 261 of
	// metronic's 501 icons. Kept raw, and truthiness asked explicitly.
	StyleAttribute json.RawMessage `json:"styleAttribute"`

	// AllowedValues is a list of illustrative value shapes, not a map and not
	// enforced by any of the four readers. Kept raw so the field round-trips
	// without this package pretending to a structure it does not use.
	AllowedValues json.RawMessage `json:"allowedValues"`

	// Derived, built once in load().
	allowedTagSet    map[string]bool
	forbiddenTagSet  map[string]bool
	allowedAttrLower map[string]bool
	fragmentPattern  *regexp.Regexp
}

var (
	loadOnce sync.Once
	loaded   *Policy
	loadErr  error
)

// Load returns the parsed policy.
//
// Deliberately not forgiving. A policy that silently fell back to an empty
// document would strip every element from every icon, and one that fell back to
// a permissive default would be a security hole; both look like a working
// install until something renders.
func Load() (*Policy, error) {
	loadOnce.Do(func() {
		var p Policy
		if err := json.Unmarshal(policyJSON, &p); err != nil {
			loadErr = fmt.Errorf("svg-policy.json: %w", err)
			return
		}
		if len(p.AllowedTags) == 0 {
			loadErr = fmt.Errorf("svg-policy.json declares no allowedTags")
			return
		}

		p.allowedTagSet = toSet(p.AllowedTags, false)
		p.forbiddenTagSet = toSet(p.ForbiddenTags, false)
		p.allowedAttrLower = toSet(p.allowedAttributeNames(), true)

		re, err := regexp.Compile(p.FragmentOnlyRefs.Allow)
		if err != nil {
			loadErr = fmt.Errorf("svg-policy.json: fragmentOnlyRefs.allow: %w", err)
			return
		}
		p.fragmentPattern = re

		loaded = &p
	})
	return loaded, loadErr
}

// MustLoad is Load for callers with nowhere to put an error.
func MustLoad() *Policy {
	p, err := Load()
	if err != nil {
		panic(err)
	}
	return p
}

// FragmentOnlyRefs names the attributes restricted to same-document
// references, and the pattern a value must match.
type FragmentOnlyRefs struct {
	Attributes []string `json:"attributes"`
	Allow      string   `json:"allow"`
}

// truthy mirrors Python's test on a JSON value: absent, null, false, 0, an
// empty string, an empty array and an empty object are all false.
func truthy(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}

func toSet(items []string, lower bool) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		if lower {
			item = strings.ToLower(item)
		}
		set[item] = true
	}
	return set
}

// allowedAttributeNames is the by-name allow-list, which is NOT simply
// allowedAttributes.
//
// The policy keeps value-restricted attributes in their own blocks -- `style`
// under styleAttribute, `href` and `xlink:href` under fragmentOnlyRefs -- so a
// reader that takes only allowedAttributes strips `style`, the sole paint
// source for 261 of metronic's 501 icons. The PHP reader and both TypeScript
// ones make the same merge; keeping all four identical is the point of having
// one policy.
//
// Being on this list means the NAME may appear. The value is still checked.
func (p *Policy) allowedAttributeNames() []string {
	names := append([]string{}, p.AllowedAttributes...)
	if truthy(p.StyleAttribute) {
		names = append(names, "style")
	}
	names = append(names, p.FragmentOnlyRefs.Attributes...)
	return names
}

// TagAllowed reports whether an element may appear.
func (p *Policy) TagAllowed(name string) bool {
	return !p.forbiddenTagSet[name] && p.allowedTagSet[name]
}

// AttributeAllowed reports whether an attribute survives, by name then value.
//
// Order matters and is the design. The deny prefix wins over everything, then
// the ARIA-style allow prefixes, then the name list, and only then the value
// checks. Being on the name list never means the value is trusted -- collapsing
// that distinction is how a sanitiser ends up permitting
// href="javascript:alert(1)" because `href` was "allowed".
func (p *Policy) AttributeAllowed(name, value string, alsoStripClass bool) bool {
	lowered := strings.ToLower(name)

	for _, prefix := range p.DenyAttributePrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return false
		}
	}

	if alsoStripClass && lowered == "class" {
		return false
	}

	for _, prefix := range p.AllowedAttributePrefix {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}

	if !p.allowedAttrLower[lowered] {
		return false
	}

	if lowered == "href" || lowered == "xlink:href" {
		return p.fragmentPattern.MatchString(value)
	}

	if lowered == "style" {
		return StyleValueIsSafe(value)
	}

	return true
}

// styleSinks are the CSS constructs that execute or fetch.
var styleSinks = []string{"expression(", "behavior:", "-moz-binding", "@import"}

// urlCall matches a CSS url() and captures its target.
var urlCall = regexp.MustCompile(`(?i)url\(\s*(['"]?)([^'")]*)['"]?\s*\)`)

// StyleValueIsSafe reports whether a style attribute value is permitted.
//
// A url() aimed off the document is the CSS exfiltration vector, so every one
// in the value has to target a same-document fragment.
func StyleValueIsSafe(value string) bool {
	lowered := strings.ToLower(value)
	for _, sink := range styleSinks {
		if strings.Contains(lowered, sink) {
			return false
		}
	}

	for _, match := range urlCall.FindAllStringSubmatch(value, -1) {
		if !strings.HasPrefix(match[2], "#") {
			return false
		}
	}
	return true
}

// AttributeName normalises a parsed attribute back to the policy's spelling.
//
// The policy names the xlink attribute `xlink:href`, and both parsers hand it
// over resolved to its namespace URI -- lxml as {http://…/xlink}href, Go as a
// Name with that Space. Everything else loses its namespace.
func AttributeName(space, local string) string {
	if space == NamespaceXLink {
		return "xlink:" + local
	}
	return local
}
