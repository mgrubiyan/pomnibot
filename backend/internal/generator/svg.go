package generator

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const maxSVGBytes = 128 * 1024
const svgNamespace = "http://www.w3.org/2000/svg"

var (
	svgElements   = wordSet("svg g defs path rect circle ellipse line polyline polygon text tspan title desc marker linearGradient radialGradient stop clipPath")
	svgAttributes = wordSet("id viewBox width height x y x1 y1 x2 y2 cx cy r rx ry d points transform opacity fill fill-opacity fill-rule stroke stroke-width stroke-opacity stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset clip-path clip-rule marker-start marker-mid marker-end markerWidth markerHeight refX refY orient markerUnits preserveAspectRatio gradientUnits gradientTransform spreadMethod offset stop-color stop-opacity fx fy fr font-family font-size font-weight font-style text-anchor dominant-baseline alignment-baseline dx dy rotate textLength lengthAdjust vector-effect")
	svgID         = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]*$`)
	svgLocalURL   = regexp.MustCompile(`^url\(#([A-Za-z_][A-Za-z0-9_.:-]*)\)$`)
	svgColor      = regexp.MustCompile(`(?i)^(?:[a-z]+|#[0-9a-f]{3,8}|(?:rgb|rgba|hsl|hsla)\([0-9.,% +\-]+\))$`)
)

func wordSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(words) {
		out[word] = true
	}
	return out
}

// validateSVG accepts a deliberately small static SVG vocabulary. No CSS,
// embedded documents, active content, or resource-loading elements are allowed.
// It validates but never rewrites the source that is persisted.
func validateSVG(source string) error {
	invalid := errors.New("invalid static SVG")
	if len(source) == 0 || len(source) > maxSVGBytes {
		return invalid
	}
	d := xml.NewDecoder(strings.NewReader(source))
	depth, nodes := 0, 0
	root, declaration := false, false
	ids, references := map[string]bool{}, []string{}
	for {
		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return invalid
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			nodes++
			if depth > 64 || nodes > 10000 || !svgElements[t.Name.Local] || (t.Name.Space != "" && t.Name.Space != svgNamespace) {
				return invalid
			}
			if depth == 1 {
				if root || t.Name.Local != "svg" {
					return invalid
				}
				root = true
			} else if t.Name.Local == "svg" {
				return invalid
			}
			attrs, hasViewBox := map[string]bool{}, false
			for _, a := range t.Attr {
				name, value := a.Name.Local, strings.TrimSpace(a.Value)
				if attrs[name] {
					return invalid
				}
				attrs[name] = true
				if a.Name.Space == "" && name == "xmlns" {
					if depth != 1 || value != svgNamespace {
						return invalid
					}
					continue
				}
				if a.Name.Space != "" || !svgAttributes[name] {
					return invalid
				}
				switch name {
				case "viewBox":
					if !validViewBox(value) {
						return invalid
					}
					hasViewBox = true
				case "id":
					if !svgID.MatchString(value) || ids[value] {
						return invalid
					}
					ids[value] = true
				case "fill", "stroke", "stop-color":
					if match := svgLocalURL.FindStringSubmatch(value); match != nil {
						references = append(references, match[1])
					} else if !svgColor.MatchString(value) {
						return invalid
					}
				case "clip-path", "marker-start", "marker-mid", "marker-end":
					if value == "none" {
						continue
					}
					match := svgLocalURL.FindStringSubmatch(value)
					if match == nil {
						return invalid
					}
					references = append(references, match[1])
				}
			}
			if depth == 1 && !hasViewBox {
				return invalid
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return invalid // Includes all DTD/entity declarations.
		case xml.ProcInst:
			if t.Target != "xml" || root || declaration {
				return invalid
			}
			declaration = true
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return invalid
			}
		}
	}
	if !root || depth != 0 {
		return invalid
	}
	for _, id := range references {
		if !ids[id] {
			return invalid
		}
	}
	return nil
}

func validViewBox(value string) bool {
	parts := strings.Fields(strings.ReplaceAll(value, ",", " "))
	if len(parts) != 4 {
		return false
	}
	for i, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (i >= 2 && n <= 0) {
			return false
		}
	}
	return true
}
