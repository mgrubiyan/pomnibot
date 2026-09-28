package ingest

import (
	"strings"
	"testing"
)

func TestPoorRecognition(t *testing.T) {
	tests := []struct {
		name string
		page OCRPage
		want bool
	}{
		{"clean text", OCRPage{Text: "Митоз — непрямое деление соматических клеток.\nАнафаза — самая короткая фаза митоза."}, false},
		{"text with formulas", OCRPage{Text: "Площадь круга S = πr², где r ≈ 3 см.\nОбъём V = 4/3 · π · r³, при r = 2 см V ≈ 33,5 см³.\n2n → n: число хромосом уменьшается вдвое (≈ 50%)."}, false},
		{"garbage characters", OCRPage{Text: "Ми¤оз ▒▓ неп░ямое ¦¦ деление ◊◊ сомат¤ческих ▓▒ клеток ░░ ¤¤"}, true},
		{"lines broken into crumbs", OCRPage{Text: strings.Repeat("М\nи\nт\nо\nз\n", 4) + "деление соматических клеток идёт непрямым путём"}, true},
		{"almost nothing", OCRPage{Text: "ми тоз"}, true},
		{"low confidence", OCRPage{Text: "Митоз — непрямое деление соматических клеток.", Confidence: 0.4, HasConfidence: true}, true},
		{"high confidence", OCRPage{Text: "¤▒", Confidence: 0.95, HasConfidence: true}, false},
	}
	for _, tt := range tests {
		if got := poorRecognition(tt.page); got != tt.want {
			t.Errorf("%s: poorRecognition() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
