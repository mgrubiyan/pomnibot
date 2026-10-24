package generator

import (
	"strings"
	"testing"
)

func TestSVGValidation(t *testing.T) {
	for _, svg := range []string{
		testSVG,
		`<?xml version="1.0"?><svg viewBox="0 0 1 1"><title>` + strings.Repeat("x", maxSVGBytes-len(`<?xml version="1.0"?><svg viewBox="0 0 1 1"><title></title></svg>`)) + `</title></svg>`,
		`<svg viewBox="0 0 10 20"><defs><marker id="arrow" viewBox="0 0 2 2"><path d="M0 0L2 1L0 2Z"/></marker></defs><line x2="10" marker-end="url(#arrow)"/><text x="1" y="2">A &amp; B</text></svg>`,
		`<svg viewBox="-1 -1 2 2"><defs><linearGradient id="g"><stop offset="0%" stop-color="#fff"/></linearGradient></defs><rect width="1" height="1" fill="url(#g)"/></svg>`,
	} {
		if err := validateSVG(svg); err != nil {
			t.Errorf("rejected valid SVG: %v", err)
		}
	}
	for _, svg := range []string{
		`<svg/>`, `<svg viewBox="0 0 0 1"/>`, `<svg viewBox="0 0 NaN 1"/>`, `<svg viewBox="0 0 1 1"><g></svg>`,
		`<svg viewBox="0 0 1 1"/><svg viewBox="0 0 1 1"/>`,
		`<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg viewBox="0 0 1 1"/>`,
		`<?xml-stylesheet href="https://example.com/a.css"?><svg viewBox="0 0 1 1"/>`,
		`<svg viewBox="0 0 1 1"><script>alert(1)</script></svg>`,
		`<svg viewBox="0 0 1 1" onload="alert(1)"/>`,
		`<svg viewBox="0 0 1 1"><foreignObject/></svg>`,
		`<svg viewBox="0 0 1 1"><animate attributeName="x"/></svg>`,
		`<svg viewBox="0 0 1 1"><image href="data:image/svg+xml;base64,AAAA"/></svg>`,
		`<svg viewBox="0 0 1 1"><use href="https://example.com/a.svg#x"/></svg>`,
		`<svg viewBox="0 0 1 1"><path fill="url(https://example.com/a.svg#g)"/></svg>`,
		`<svg viewBox="0 0 1 1"><path fill="u&#114;l(https://example.com/g)"/></svg>`,
		`<svg viewBox="0 0 1 1" style="background:url(https://example.com)"/>`,
		`<svg viewBox="0 0 1 1"><style>@import url(https://example.com);</style></svg>`,
		`<svg xmlns="https://evil.example" viewBox="0 0 1 1"/>`,
		`<svg viewBox="0 0 1 1" xmlns:x="http://www.w3.org/1999/xlink"><path x:href="x"/></svg>`,
		`<svg viewBox="0 0 1 1" viewBox="0 0 2 2"/>`,
		`<svg viewBox="0 0 1 1"><path clip-path="url(#missing)"/></svg>`,
		`<svg viewBox="0 0 1 1"><g id="duplicate"/><g id="duplicate"/></svg>`,
		`<svg viewBox="0 0 1 1">` + strings.Repeat("<g>", 65) + strings.Repeat("</g>", 65) + `</svg>`,
		`<svg viewBox="0 0 1 1">` + strings.Repeat("x", 128*1024) + `</svg>`,
	} {
		if err := validateSVG(svg); err == nil {
			t.Errorf("accepted unsafe or invalid SVG: %.100s", svg)
		}
	}
}
