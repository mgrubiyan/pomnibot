package material

import "testing"

func TestEvidenceCannotJoinDistantSpansOrCiteInterpretations(t *testing.T) {
	d := Document{Blocks: []Block{{ID: "p1-b1", Page: 1, Text: "SQLite допускает динамическую типизацию."}, {ID: "p3-b1", Page: 3, Text: "Не все СУБД поддерживают это правило.", Observations: "Возможно, стрелка означает наследование."}}}
	for _, e := range []Evidence{{BlockID: "p1-b1", Quote: "SQLite допускает динамическую типизацию. Не все СУБД"}, {BlockID: "p3-b1", Quote: "стрелка означает наследование"}, {BlockID: "missing", Quote: "SQLite"}} {
		if d.ValidEvidence(e) {
			t.Fatalf("accepted fabricated evidence: %+v", e)
		}
	}
	if !d.ValidEvidence(Evidence{BlockID: "p3-b1", Quote: "Не все СУБД поддерживают это правило."}) {
		t.Fatal("lost exact separate source")
	}
}

func TestTableEvidencePreservesCellCoordinates(t *testing.T) {
	d := Document{Blocks: []Block{{ID: "t", Page: 17, Kind: "table", Cells: [][]string{{"Тип", "Размер"}, {"INT", "4"}, {"CHAR", "1"}}}}}
	if !d.ValidEvidence(Evidence{BlockID: "t", Row: 2, Column: 0, Quote: "CHAR"}) {
		t.Fatal("valid cell lost")
	}
	if d.ValidEvidence(Evidence{BlockID: "t", Row: 1, Column: 2, Quote: "CHAR"}) {
		t.Fatal("accepted wrong row/column")
	}
}

func TestVisualEvidenceRequiresOriginalAndCannotPretendToBeQuotation(t *testing.T) {
	r := Region{0, 0, 1, 1}
	d := Document{Blocks: []Block{{ID: "diagram", Page: 3, Kind: "diagram", Text: "R1 R2", Region: &r}}, Pages: []Page{{Number: 3, Image: []byte("original")}}}
	e := Evidence{BlockID: "diagram", Kind: "image", Region: &r, Observation: "Стрелка от R1 к R2"}
	if !d.ValidEvidence(e) {
		t.Fatal("valid visual anchor lost")
	}
	e.Quote = "Стрелка от R1 к R2"
	if d.ValidEvidence(e) {
		t.Fatal("visual interpretation accepted as literal quote")
	}
	e.Quote = ""
	d.Pages[0].Image = nil
	if d.ValidEvidence(e) {
		t.Fatal("visual anchor accepted without original image")
	}
}

func TestEvidenceRejectsUnknownKinds(t *testing.T) {
	d := Document{Blocks: []Block{{ID: "a", Text: "Actual source."}}}
	if d.ValidEvidence(Evidence{BlockID: "a", Kind: "invented", Quote: "Actual source."}) {
		t.Fatal("unknown evidence kind accepted")
	}
}
