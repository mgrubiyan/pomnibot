package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
)

// XML order and native table cells are preserved. A connector's object IDs are
// source data, not an inferred semantic relation. Without a slide renderer,
// embedded-image reading cannot establish full-slide spatial relationships.
type officeNode struct {
	Name     xml.Name     `xml:"-"`
	Attrs    []xml.Attr   `xml:",any,attr"`
	Text     string       `xml:",chardata"`
	Children []officeNode `xml:",any"`
}

func (n *officeNode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type alias officeNode
	n.Name = start.Name
	return d.DecodeElement((*alias)(n), &start)
}
func (n officeNode) attr(key string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
func (n officeNode) find(name string) []officeNode {
	var out []officeNode
	if n.Name.Local == name {
		return []officeNode{n}
	}
	for _, c := range n.Children {
		out = append(out, c.find(name)...)
	}
	return out
}
func (n officeNode) text() string {
	var lines []string
	for _, p := range n.find("p") {
		var line strings.Builder
		for _, t := range p.find("t") {
			line.WriteString(t.Text)
		}
		if line.Len() > 0 {
			lines = append(lines, line.String())
		}
	}
	return strings.Join(lines, "\n")
}
func readOfficeNode(f *zip.File) (officeNode, error) {
	r, err := f.Open()
	if err != nil {
		return officeNode{}, err
	}
	defer func() { _ = r.Close() }()
	var root officeNode
	err = xml.NewDecoder(io.LimitReader(r, maxOfficePart)).Decode(&root)
	return root, err
}

type nativeSlide struct {
	text   string
	blocks []material.Block
	issues []string
}

func (e *Extractor) structuredSlides(ctx context.Context, data []byte) ([]nativeSlide, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	var names []string
	for _, f := range zr.File {
		files[f.Name] = f
		if reSlide.MatchString(f.Name) {
			names = append(names, f.Name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, _ := strconv.Atoi(reSlide.FindStringSubmatch(names[i])[1])
		b, _ := strconv.Atoi(reSlide.FindStringSubmatch(names[j])[1])
		return a < b
	})
	if presentation, ok := files["ppt/presentation.xml"]; ok && files["ppt/_rels/presentation.xml.rels"] != nil {
		root, err := readOfficeNode(presentation)
		if err != nil {
			return nil, err
		}
		relationships, err := readOfficeNode(files["ppt/_rels/presentation.xml.rels"])
		if err != nil {
			return nil, err
		}
		targets := map[string]string{}
		for _, r := range relationships.find("Relationship") {
			if r.attr("TargetMode") != "External" {
				targets[r.attr("Id")] = path.Clean(path.Join("ppt", r.attr("Target")))
			}
		}
		var declared []string
		for _, slide := range root.find("sldId") {
			relationshipID := ""
			for _, a := range slide.Attrs {
				if a.Name.Local == "id" && a.Name.Space != "" {
					relationshipID = a.Value
				}
			}
			name := targets[relationshipID]
			if files[name] == nil {
				return nil, fmt.Errorf("presentation references a missing slide")
			}
			declared = append(declared, name)
		}
		if len(declared) > 0 {
			names = declared
		}
	}
	var out []nativeSlide
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		root, err := readOfficeNode(files[name])
		if err != nil {
			return nil, err
		}
		trees := root.find("spTree")
		if len(trees) == 0 {
			out = append(out, nativeSlide{text: root.text()})
			continue
		}
		rels := map[string]string{}
		if f := files[path.Join(path.Dir(name), "_rels", path.Base(name)+".rels")]; f != nil {
			r, err := readOfficeNode(f)
			if err != nil {
				return nil, err
			}
			for _, rel := range r.find("Relationship") {
				if rel.attr("TargetMode") != "External" {
					rels[rel.attr("Id")] = path.Clean(path.Join(path.Dir(name), rel.attr("Target")))
				}
			}
		}
		s := nativeSlide{}
		for _, shape := range trees[0].Children {
			if tables := shape.find("tbl"); len(tables) > 0 {
				for _, table := range tables {
					b := material.Block{Kind: "table"}
					var rows []string
					for _, row := range table.find("tr") {
						var cells []string
						for _, cell := range row.find("tc") {
							cells = append(cells, cell.text())
						}
						b.Cells = append(b.Cells, cells)
						rows = append(rows, strings.Join(cells, " | "))
					}
					b.Text = strings.Join(rows, "\n")
					s.blocks = append(s.blocks, b)
				}
				continue
			}
			if shape.Name.Local == "cxnSp" {
				s.issues = append(s.issues, "Есть соединители между объектами; полная пространственная схема требует PDF/изображения слайда.")
				s.blocks = append(s.blocks, material.Block{Kind: "diagram", Observations: "В исходнике есть соединитель между объектами; семантика не установлена.", Issues: []string{"Соединитель не проверен по полному изображению слайда"}})
				continue
			}
			if shape.Name.Local == "pic" {
				for _, blip := range shape.find("blip") {
					f := files[rels[blip.attr("embed")]]
					if f == nil || e.ocr == nil {
						s.issues = append(s.issues, "Изображение внутри слайда не прочитано; пришлите PDF для полного визуального контекста.")
						continue
					}
					r, err := f.Open()
					if err != nil {
						return nil, err
					}
					data, readErr := io.ReadAll(io.LimitReader(r, maxOfficePart+1))
					_ = r.Close()
					if readErr != nil {
						return nil, readErr
					}
					if len(data) > maxOfficePart {
						return nil, fmt.Errorf("embedded slide image exceeds size limit")
					}
					fitted, mime, _, fitErr := fitImage(data, e.lim)
					if fitErr != nil {
						s.issues = append(s.issues, "Формат изображения внутри слайда не распознан")
						continue
					}
					pages, ocrErr := e.ocr.Recognize(ctx, OCRRequest{Data: fitted, MimeType: mime, Model: e.model(ModelScan), Pages: 1})
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if ocrErr != nil || len(pages) == 0 {
						s.issues = append(s.issues, "Не удалось прочитать изображение внутри слайда")
						continue
					}
					p := pages[0]
					s.blocks = append(s.blocks, material.Block{Kind: "image", Text: p.Text, Observations: p.Observations, Issues: p.Issues})
					s.issues = append(s.issues, "Прочитано вложенное изображение; взаимное расположение объектов слайда не проверено.")
				}
				continue
			}
			if text := shape.text(); text != "" {
				s.blocks = append(s.blocks, material.Block{Kind: "text", Text: text})
			}
		}
		var texts []string
		for _, b := range s.blocks {
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		s.text = strings.Join(texts, "\n")
		out = append(out, s)
	}
	return out, nil
}
