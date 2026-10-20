package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

var i18nKeyLine = regexp.MustCompile(`(?m)^\s*"([^"]+)":`)
var i18nCall = regexp.MustCompile(`\bt\("([^"]+)"`)

func i18nDict(t *testing.T, src, name string) map[string]bool {
	start := strings.Index(src, "const "+name+" = {\n")
	if start < 0 {
		t.Fatalf("dictionary %s not found", name)
	}
	end := strings.Index(src[start:], "\n};")
	if end < 0 {
		t.Fatalf("dictionary %s not closed", name)
	}
	keys := map[string]bool{}
	for _, m := range i18nKeyLine.FindAllStringSubmatch(src[start:start+end], -1) {
		if keys[m[1]] {
			t.Errorf("%s: duplicate key %q", name, m[1])
		}
		keys[m[1]] = true
	}
	return keys
}

func TestI18nKeysMatch(t *testing.T) {
	raw, err := webFS.ReadFile("web/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	en, de := i18nDict(t, src, "en"), i18nDict(t, src, "de")
	var missing []string
	for k := range en {
		if !de[k] {
			missing = append(missing, "de lacks "+k)
		}
	}
	for k := range de {
		if !en[k] {
			missing = append(missing, "en lacks "+k)
		}
	}
	entries, err := webFS.ReadDir("web")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		body, _ := webFS.ReadFile("web/" + e.Name())
		for _, m := range i18nCall.FindAllStringSubmatch(string(body), -1) {
			if !en[m[1]] {
				missing = append(missing, e.Name()+" uses unknown key "+m[1])
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Error(m)
	}
}
