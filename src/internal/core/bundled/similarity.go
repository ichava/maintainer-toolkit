package bundled

import (
	"os"
	"path/filepath"
	"sort"
)

// TieBreakMargin is how far ahead the closest directory must be before a tie
// counts as decided.
//
// 1.15 was set from the five ties the aggregator pack actually contains.
// Four are decided by a wide margin -- cryptocurrency-icons 0.97 against 0.64,
// pepicons 0.98 against 0.19, fontawesome 0.37 against 0.14 -- and ionicons is
// the closest genuine call at 0.42 against 0.34, a ratio of 1.22. Anything
// tighter than this is a judgement, and a judgement belongs to a human.
const TieBreakMargin = 1.15

// similaritySample caps how many icons are compared per directory. The
// candidates are whole icon sets, so the mean settles long before the sample
// is exhausted and reading thousands of files per rival would dominate the
// audit's runtime for no extra confidence.
const similaritySample = 24

// Similarity scores how closely a directory's icons resemble the committed
// ones, as a mean over the icons the two have in common.
//
// This exists because byte equality answers nothing here. When a package ships
// sibling variant directories -- black/white/color, solid/outline,
// pencil/pop/print -- every one of them has the same filenames, so name
// coverage grades them identically, and when the pack vendored an older
// release none of them is byte-identical either. The scorer is then choosing
// by directory-walk order, which is not a choice.
//
// Resemblance separates the common case, where the variants are different
// drawings: measured on the aggregator pack it settles fontawesome onto svgs
// rather than svgs-full (FA7's 640x640 geometry against the committed 448x512)
// and pepicons onto svg/pop rather than svg/print.
//
// It deliberately does not settle everything, and the limit is worth knowing
// because it is structural rather than a tuning problem. Trigrams over a whole
// file are dominated by the path data, so two variants that draw the identical
// shape and differ only in colour attributes score nearly the same. That is
// exactly cryptocurrency-icons -- svg/black and svg/white share every path and
// differ by one fill -- and it is why those stay ambiguous rather than being
// settled by a metric that cannot see the difference. Reporting the tie is the
// correct outcome there; a threshold tuned until it happened to pick black
// would be fitting the constant to two cases and would settle the next one
// wrongly and silently.
func Similarity(dir string, fp SetFingerprint) float64 {
	if fp.Root == "" {
		return 0
	}

	keys := make([]string, 0, len(fp.Hashes))
	for k := range fp.Hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var total float64
	var n int
	for _, key := range keys {
		if n >= similaritySample {
			break
		}
		committed, err := readIcon(fp.Root, key)
		if err != nil {
			continue
		}
		candidate, err := readIcon(dir, key)
		if err != nil {
			continue
		}
		total += trigramJaccard(committed, candidate)
		n++
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}

// readIcon resolves a fingerprint key back to a file. Keys drop the extension,
// and the two sides do not always agree on its case.
func readIcon(root, key string) ([]byte, error) {
	for _, ext := range []string{".svg", ".SVG"} {
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)+ext)); err == nil {
			return b, nil
		}
	}
	return nil, os.ErrNotExist
}

// trigramJaccard measures overlap on byte trigrams.
//
// Chosen over an edit distance because it is linear and order-insensitive, so
// an attribute moved from the root element onto each path -- which is how
// these packs routinely differ from their upstreams -- barely moves the score
// while a different drawing moves it a lot. The cost of that choice is stated
// in Similarity: it cannot separate two variants of the same drawing.
func trigramJaccard(a, b []byte) float64 {
	sa, sb := trigrams(a), trigrams(b)
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}

	small, large := sa, sb
	if len(large) < len(small) {
		small, large = large, small
	}
	shared := 0
	for g := range small {
		if _, ok := large[g]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(sa)+len(sb)-shared)
}

func trigrams(b []byte) map[[3]byte]struct{} {
	out := make(map[[3]byte]struct{}, len(b))
	for i := 0; i+3 <= len(b); i++ {
		out[[3]byte{b[i], b[i+1], b[i+2]}] = struct{}{}
	}
	return out
}
