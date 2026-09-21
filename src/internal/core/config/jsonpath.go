package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// This file is the whole reason the package has no JSON dependency.
//
// Reading a dotted path is trivial -- unmarshal and walk. Writing one is not,
// and the naive version is a trap: Go map iteration has no order, so
// unmarshal-modify-marshal reorders every key in the document. The pack configs
// this writes into are committed and reviewed on a sync PR, so a one-line
// version bump would arrive as a whole-file rewrite and nobody would read it.
//
// So the setter finds the existing value's byte range and splices. Everything
// it does not touch stays byte-identical, including key order, indentation and
// trailing newline.

// getStringAtPath returns the string at a dotted path, and whether it was a
// string. A path through a non-object, or a missing segment, is not found.
func getStringAtPath(data []byte, dotted string) (string, bool) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return "", false
	}

	cursor := root
	for _, seg := range strings.Split(dotted, ".") {
		obj, ok := cursor.(map[string]any)
		if !ok {
			return "", false
		}
		cursor, ok = obj[seg]
		if !ok {
			return "", false
		}
	}

	s, ok := cursor.(string)
	return s, ok
}

// setStringAtPath replaces the string at a dotted path, preserving the rest of
// the document byte for byte.
//
// It reports ok=false when the path does not already resolve to a string. It
// deliberately never creates a key: `icon-sets-bundled` ships no
// `package.upstream_version`, and inventing one there would write a field the
// pack does not declare. Python had the same rule.
func setStringAtPath(data []byte, dotted, value string) (out []byte, ok bool, err error) {
	start, end, found, err := locateValue(data, strings.Split(dotted, "."))
	if err != nil || !found {
		return data, false, err
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return data, false, err
	}

	spliced := make([]byte, 0, len(data)-(end-start)+len(encoded))
	spliced = append(spliced, data[:start]...)
	spliced = append(spliced, encoded...)
	spliced = append(spliced, data[end:]...)
	return spliced, true, nil
}

// locateValue returns the byte range of the string value at path.
//
// It walks with a json.Decoder rather than a regexp because a key name can
// appear inside another string, at another depth, or more than once -- all of
// which a pattern match would hit and this cannot.
func locateValue(data []byte, path []string) (start, end int, found bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))

	tok, err := dec.Token()
	if err != nil {
		return 0, 0, false, err
	}
	if delim, isDelim := tok.(json.Delim); !isDelim || delim != '{' {
		return 0, 0, false, nil
	}

	return walkObject(dec, data, path)
}

// walkObject consumes one object, descending into path[0] when it matches.
func walkObject(dec *json.Decoder, data []byte, path []string) (start, end int, found bool, err error) {
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}
		key, isString := keyTok.(string)
		if !isString {
			return 0, 0, false, fmt.Errorf("object key is not a string: %v", keyTok)
		}

		if len(path) > 0 && key == path[0] {
			if len(path) == 1 {
				valueTok, err := dec.Token()
				if err != nil {
					return 0, 0, false, err
				}
				if _, isString := valueTok.(string); !isString {
					// The path resolves, but not to a string. Treat as absent:
					// `package.version` is a number in some packs and must not
					// be rewritten into a string.
					return 0, 0, false, nil
				}
				end = int(dec.InputOffset())
				start, err = stringStart(data, end)
				if err != nil {
					return 0, 0, false, err
				}
				return start, end, true, nil
			}

			nextTok, err := dec.Token()
			if err != nil {
				return 0, 0, false, err
			}
			delim, isDelim := nextTok.(json.Delim)
			if !isDelim || delim != '{' {
				return 0, 0, false, nil // path continues through a non-object
			}
			start, end, found, err = walkObject(dec, data, path[1:])
			if err != nil || found {
				return start, end, found, err
			}
			if err := closeValue(dec); err != nil {
				return 0, 0, false, err
			}
			continue
		}

		if err := skipValue(dec); err != nil {
			return 0, 0, false, err
		}
	}

	// Consume this object's closing brace so the caller's loop stays aligned.
	if _, err := dec.Token(); err != nil {
		return 0, 0, false, err
	}
	return 0, 0, false, nil
}

// skipValue consumes the next value, whatever its shape.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, isDelim := tok.(json.Delim); isDelim && (delim == '{' || delim == '[') {
		return closeValue(dec)
	}
	return nil
}

// closeValue consumes the remainder of an already-opened object or array.
func closeValue(dec *json.Decoder) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, isDelim := tok.(json.Delim); isDelim {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// stringStart walks back from the byte after a JSON string to its opening
// quote, skipping quotes that are themselves escaped.
func stringStart(data []byte, end int) (int, error) {
	if end <= 0 || end > len(data) || data[end-1] != '"' {
		return 0, fmt.Errorf("value ending at offset %d is not a quoted string", end)
	}
	for i := end - 2; i >= 0; i-- {
		if data[i] != '"' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && data[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no opening quote for string ending at offset %d", end)
}
