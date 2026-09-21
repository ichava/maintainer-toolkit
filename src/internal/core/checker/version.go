package checker

import (
	"strconv"
	"strings"
)

// Version ordering, and why it is PHP's rather than Python's.
//
// core/checker.py opens with "Mirrors PHP IconPackUpdateChecker exactly. The
// two implementations MUST agree." They do not. PHP's isStale() calls
// version_compare($current, $latest, '<'); Python's builds a tuple of mixed
// ints and strings and compares that, under a docstring claiming it "falls
// back to lexical safely".
//
// The two disagree in the direction that causes harm. For 3.46.0 against
// 3.46.0-rc1, PHP orders the release *above* its own release candidate and
// reports not-stale; Python's tuple comparison makes (3,46,0) < (3,46,0,'rc1')
// true and reports stale, so a scheduled sync would propose replacing a stable
// release with a pre-release of itself. Python's version also raises TypeError
// outright when comparing an int part against a string part, which is a crash
// rather than an answer.
//
// PHP is the declared reference and is the correct one, so this port matches
// PHP. That is a deliberate behaviour change against the Python it replaces,
// and it is pinned by a differential test that runs the real `php -r
// 'version_compare(...)'` over a corpus of real and adversarial pairs.

var specialForms = map[string]int{
	"dev":   0,
	"alpha": 1, "a": 1,
	"beta": 2, "b": 2,
	"RC": 3, "rc": 3,
	"#":  4,
	"pl": 5, "p": 5,
}

// specialFormOrder ranks a non-numeric part. Anything unrecognised sorts below
// "dev", which is what PHP's -1 default does.
func specialFormOrder(part string) int {
	if order, ok := specialForms[part]; ok {
		return order
	}
	return -1
}

// canonicalizeVersion mirrors PHP's php_canonicalize_version: it replaces
// '-', '_' and '+' with '.', and inserts a '.' at every digit/non-digit
// boundary, so "1.0rc1" and "1.0-rc-1" both become "1.0.rc.1".
func canonicalizeVersion(v string) []string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '-' || c == '_' || c == '+' {
			b.WriteByte('.')
			continue
		}
		if i > 0 {
			prev := v[i-1]
			prevDigit, curDigit := isDigit(prev), isDigit(c)
			// A boundary between a digit and a letter starts a new part, but
			// only when neither side is already a separator.
			if prev != '.' && c != '.' && prevDigit != curDigit {
				b.WriteByte('.')
			}
		}
		b.WriteByte(c)
	}

	parts := strings.Split(b.String(), ".")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNumericPart(p string) bool {
	if p == "" {
		return false
	}
	for i := 0; i < len(p); i++ {
		if !isDigit(p[i]) {
			return false
		}
	}
	return true
}

// comparePart compares two canonicalized parts the way PHP does: numerically
// when both are numeric, by special form when neither is, and by treating the
// numeric side as "#" when they are mixed.
func comparePart(a, b string) int {
	aNum, bNum := isNumericPart(a), isNumericPart(b)

	switch {
	case aNum && bNum:
		// ParseInt handles leading zeros, so "007" and "7" compare equal, as
		// they do in PHP. A part long enough to overflow int64 falls back to
		// comparing by length then lexically, which keeps the ordering sane
		// rather than silently wrapping to a negative.
		x, errA := strconv.ParseInt(a, 10, 64)
		y, errB := strconv.ParseInt(b, 10, 64)
		if errA != nil || errB != nil {
			ta, tb := strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
			if len(ta) != len(tb) {
				return sign(int64(len(ta) - len(tb)))
			}
			return strings.Compare(ta, tb)
		}
		return sign(x - y)
	case !aNum && !bNum:
		return sign(int64(specialFormOrder(a) - specialFormOrder(b)))
	case aNum:
		return sign(int64(specialFormOrder("#") - specialFormOrder(b)))
	default:
		return sign(int64(specialFormOrder(a) - specialFormOrder("#")))
	}
}

func sign(n int64) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// VersionCompare returns -1, 0 or 1 for a<b, a==b, a>b, matching PHP's
// version_compare with no operator.
//
// When one version runs out of parts first, PHP decides by the next part of
// the longer one: a numeric part or one ranking at or above "#" makes the
// longer version greater (1.0.1 > 1.0), and anything below -- dev, alpha,
// beta, RC -- makes it lesser (1.0-rc1 < 1.0).
func VersionCompare(a, b string) int {
	partsA, partsB := canonicalizeVersion(a), canonicalizeVersion(b)

	n := len(partsA)
	if len(partsB) < n {
		n = len(partsB)
	}
	for i := 0; i < n; i++ {
		if c := comparePart(partsA[i], partsB[i]); c != 0 {
			return c
		}
	}

	if len(partsA) == len(partsB) {
		return 0
	}

	longer, result := partsB, -1
	if len(partsA) > len(partsB) {
		longer, result = partsA, 1
	}
	next := longer[n]
	if isNumericPart(next) || specialFormOrder(next) >= specialFormOrder("#") {
		return result
	}
	return -result
}

// TrimVersion strips a leading "v" from a tag.
//
// Both existing implementations use a character-set trim -- PHP's
// ltrim($tag, 'vV ') and Python's lstrip("vV ") -- which also eats a leading
// "V" or space, and would turn "Version1" into "ersion1". That is a latent
// oddity rather than a live defect: no registry these packs poll emits such a
// tag. It is preserved rather than quietly corrected, because changing it here
// would put this port out of step with *both* of the implementations it has to
// agree with, and the PHP side is the one that decides.
func TrimVersion(tag string) string {
	return strings.TrimSpace(strings.TrimLeft(tag, "vV "))
}

// IsStale reports whether current is behind latest. An unknown current is
// stale: a pack with no vendored version has never been synced.
func IsStale(current, latest string) bool {
	if current == "" {
		return true
	}
	return VersionCompare(current, latest) < 0
}
