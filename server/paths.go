package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSegmentBytes = 120

func safeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('_')
		case unicode.IsControl(r) || r == utf8.RuneError:
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.Trim(out, ". ")
	for len(out) > maxSegmentBytes {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	out = strings.Trim(out, ". ")
	if out == "" {
		return "_"
	}
	return out
}
