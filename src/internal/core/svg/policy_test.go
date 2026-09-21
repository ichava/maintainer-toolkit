package svg

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// Ported from tests/unit/test_svg_policy.py. These are the security contract,
// not examples: each one is a construct that has to survive or has to die.

// TestVendoredPolicyHasNotDrifted pins the copy against the digest it was
// synced at. A per-repo test can only catch an accidental edit here; it cannot
// see that core has moved on, which is what .scripts/sync-svg-policy.mjs is
// for -- that script is the cross-repo gate and now lists this copy too.
func TestVendoredPolicyHasNotDrifted(t *testing.T) {
	const want = "8794a59bdf3fe3112eccc68c157d85c1c55728299dedcdbd7447ccbc083a6be2"

	sum := sha256.Sum256(policyJSON)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("svg-policy.json digest = %s, want %s\n"+
			"edit core/resources/security/svg-policy.json, run "+
			".scripts/sync-svg-policy.mjs --write, then update this digest", got, want)
	}
}

// TestMergesTheValueRestrictedNamesIntoTheAllowList is the trap the policy's
// own comments warn about: `style` is not in allowedAttributes, it lives under
// styleAttribute, and a reader that takes only allowedAttributes strips it --
// removing the sole paint source from 261 of metronic's 501 icons.
func TestMergesTheValueRestrictedNamesIntoTheAllowList(t *testing.T) {
	p := MustLoad()

	for _, name := range []string{"style", "href", "xlink:href"} {
		if !p.allowedAttrLower[name] {
			t.Errorf("%q must be on the merged allow-list", name)
		}
	}

	// And the thing that makes it a merge rather than a copy.
	for _, raw := range p.AllowedAttributes {
		if strings.EqualFold(raw, "style") {
			t.Error("style is in allowedAttributes; the merge is no longer load-bearing, so this test no longer guards it")
		}
	}
}

// TestStyleAttributeIsAnObjectNotABoolean pins the shape that broke this port
// once. Python reads styleAttribute with a truthiness test, so it works for an
// object; typing it as a Go bool made the whole policy fail to unmarshal.
func TestStyleAttributeIsAnObjectNotABoolean(t *testing.T) {
	p := MustLoad()
	if !truthy(p.StyleAttribute) {
		t.Fatal("styleAttribute must be truthy, or `style` drops off the allow-list")
	}
	if trimmed := strings.TrimSpace(string(p.StyleAttribute)); !strings.HasPrefix(trimmed, "{") {
		t.Errorf("styleAttribute is %s; if it became a boolean, simplify the reader deliberately", trimmed)
	}
}

func TestStripsAnEventHandlerHoweverItIsSpelled(t *testing.T) {
	for _, doc := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0" onload="x()"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0" onload='x()'/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0" ONLOAD="x()"/></svg>`,
	} {
		out, removed, err := SanitiseBytes([]byte(doc), false)
		if err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		if strings.Contains(strings.ToLower(string(out)), "onload") {
			t.Errorf("event handler survived: %s", out)
		}
		if !strings.Contains(string(out), `d="M0 0"`) {
			t.Errorf("path data was collateral damage: %s", out)
		}
		if len(removed) == 0 {
			t.Errorf("removal went unreported for %s", doc)
		}
	}
}

func TestParityWithTheOtherRuntimes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		doc     string
		survive []string
		die     []string
	}{
		{"keeps a literal style value",
			`<svg xmlns="http://www.w3.org/2000/svg"><path style="fill:#123456"/></svg>`,
			[]string{"style"}, nil},
		{"drops a style that fetches",
			`<svg xmlns="http://www.w3.org/2000/svg"><path style="fill:url(https://evil.test/x)"/></svg>`,
			nil, []string{"style"}},
		{"keeps a same-document use",
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#ok"/></svg>`,
			[]string{"xlink:href", "#ok"}, nil},
		{"drops a remote use",
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="https://evil.test/x"/></svg>`,
			nil, []string{"evil.test"}},
		{"keeps gradient geometry and stop-color",
			`<svg xmlns="http://www.w3.org/2000/svg"><linearGradient x1="0"><stop stop-color="#fff"/></linearGradient></svg>`,
			[]string{"x1", "stop-color", "linearGradient"}, nil},
		// The filter primitive survives as an element and loses its
		// parameters, because the policy allows all 22 fe* tags and none of
		// their attributes -- stdDeviation, in, result, values and the rest
		// are absent from allowedAttributes. So a blur radius becomes zero.
		//
		// Verified against the Python: it produces byte-identical output, so
		// this is the policy's behaviour rather than a port defect. It is
		// worth someone deciding on deliberately, but not here: the canonical
		// file lives in core, four runtimes read it, and the digest is pinned.
		{"keeps a filter primitive as an element, and strips its parameters",
			`<svg xmlns="http://www.w3.org/2000/svg"><filter><feGaussianBlur stdDeviation="2"/></filter></svg>`,
			[]string{"feGaussianBlur"}, []string{"stdDeviation"}},
		{"keeps the accessible name wiring",
			`<svg xmlns="http://www.w3.org/2000/svg" aria-labelledby="t"><title id="t">Home</title></svg>`,
			[]string{"aria-labelledby", "<title", "Home"}, nil},
		{"drops the style ELEMENT while keeping the style ATTRIBUTE",
			`<svg xmlns="http://www.w3.org/2000/svg"><style>a{}</style><path style="fill:#000"/></svg>`,
			[]string{`style="fill:#000"`}, []string{"<style>"}},
		{"drops a foreignObject",
			`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><b/></foreignObject><path d="M0 0"/></svg>`,
			[]string{"<path"}, []string{"foreignObject"}},
		{"drops a script",
			`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><path d="M0 0"/></svg>`,
			[]string{"<path"}, []string{"script", "alert"}},
		{"drops a javascript href",
			`<svg xmlns="http://www.w3.org/2000/svg"><use href="javascript:alert(1)"/></svg>`,
			nil, []string{"javascript"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := SanitiseBytes([]byte(tc.doc), false)
			if err != nil {
				t.Fatal(err)
			}
			got := string(out)
			for _, want := range tc.survive {
				if !strings.Contains(got, want) {
					t.Errorf("%q should have survived:\n%s", want, got)
				}
			}
			for _, unwanted := range tc.die {
				if strings.Contains(got, unwanted) {
					t.Errorf("%q should have been removed:\n%s", unwanted, got)
				}
			}
		})
	}
}

// TestNamespaceDeclarationsSurvive is the Go-specific trap. lxml keeps them
// out of el.attrib entirely so the Python filter never sees them; Go's decoder
// hands them over as ordinary attributes, and `xmlns` is not on the allow-list.
// Filtering them would strip the SVG namespace from every icon in the estate.
func TestNamespaceDeclarationsSurvive(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#a"/></svg>`

	out, removed, err := SanitiseBytes([]byte(doc), false)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.Contains(got, `xmlns="http://www.w3.org/2000/svg"`) {
		t.Errorf("the SVG namespace was stripped:\n%s", got)
	}
	if !strings.Contains(got, `xmlns:xlink=`) {
		t.Errorf("the xlink namespace was stripped, so xlink:href no longer resolves:\n%s", got)
	}
	if len(removed) != 0 {
		t.Errorf("namespace declarations were reported as removals: %v", removed)
	}
}

func TestRefusesRatherThanGuessesAtUnparsableInput(t *testing.T) {
	// Unquoted attribute. Deliberately unlike the runtime parser in
	// ichava/core, which recovers from this: that one runs at render time,
	// this one runs before a file is committed.
	if _, _, err := SanitiseBytes([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d=M0 0/></svg>`), false); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestAlsoStripClass(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg"><path class="icon" d="M0 0"/></svg>`

	kept, _, _ := SanitiseBytes([]byte(doc), false)
	if !strings.Contains(string(kept), `class="icon"`) {
		t.Errorf("class should survive by default: %s", kept)
	}

	stripped, _, _ := SanitiseBytes([]byte(doc), true)
	if strings.Contains(string(stripped), "class") {
		t.Errorf("class should go when asked: %s", stripped)
	}
	if !strings.Contains(string(stripped), `d="M0 0"`) {
		t.Errorf("path data was collateral damage: %s", stripped)
	}
}

// TestAttributeOrderingIsTheDesign pins the sequence the Python docstring
// calls out: deny prefix, then class, then allow prefixes, then the name list,
// then the value checks.
func TestAttributeOrderingIsTheDesign(t *testing.T) {
	p := MustLoad()

	for _, tc := range []struct {
		name, value string
		strip, want bool
	}{
		{"onclick", "x()", false, false},
		{"aria-labelledby", "t", false, true},
		{"href", "#ok", false, true},
		{"href", "javascript:alert(1)", false, false},
		{"xlink:href", "#ok", false, true},
		{"xlink:href", "https://evil.test/x", false, false},
		{"style", "fill:#fff", false, true},
		{"style", "background:url(https://evil.test/x)", false, false},
		{"style", "x:expression(alert(1))", false, false},
		{"style", "fill:url(#local)", false, true},
		{"class", "icon", false, true},
		{"class", "icon", true, false},
		{"nonsense", "x", false, false},
	} {
		if got := p.AttributeAllowed(tc.name, tc.value, tc.strip); got != tc.want {
			t.Errorf("AttributeAllowed(%q, %q, strip=%v) = %v, want %v",
				tc.name, tc.value, tc.strip, got, tc.want)
		}
	}
}

// TestLatin1IsDecodedNotRefused covers the 261 metronic flags that declare
// iso-8859-1. Go's decoder refuses an encoding it has no table for; lxml has
// them all, so this divergence existed until the corpus test found it.
func TestLatin1IsDecodedNotRefused(t *testing.T) {
	doc := []byte("<?xml version=\"1.0\" encoding=\"iso-8859-1\"?>" +
		"<svg xmlns=\"http://www.w3.org/2000/svg\"><title>Espa\xf1a</title></svg>")

	out, _, err := SanitiseBytes(doc, false)
	if err != nil {
		t.Fatalf("iso-8859-1 was refused: %v", err)
	}
	if !strings.Contains(string(out), "España") {
		t.Errorf("Latin-1 text was not decoded to UTF-8: %s", out)
	}
}

func TestAnUnknownEncodingIsAnErrorNotAGuess(t *testing.T) {
	doc := []byte(`<?xml version="1.0" encoding="shift_jis"?><svg xmlns="http://www.w3.org/2000/svg"/>`)
	if _, _, err := SanitiseBytes(doc, false); err == nil {
		t.Fatal("an unsupported encoding must be reported, not decoded as Latin-1")
	}
}

func TestStyleValueIsSafe(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"fill:#123456", true},
		{"fill:url(#local)", true},
		{"fill:url('#local')", true},
		{"fill:url(https://evil.test/x)", false},
		{"x:expression(alert(1))", false},
		{"behavior:url(#x)", false},
		{"-moz-binding:url(#x)", false},
		{"@import url(#x)", false},
		{"FILL:URL(HTTPS://EVIL.TEST/X)", false},
		{"", true},
	} {
		if got := StyleValueIsSafe(tc.value); got != tc.want {
			t.Errorf("StyleValueIsSafe(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
