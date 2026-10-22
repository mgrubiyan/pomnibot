package usecase

import (
	"errors"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

func TestValidateCreateCard(t *testing.T) {
	table := contracts.TableLayout{Columns: []string{"A", "B"}, Items: []contracts.TableItem{{Text: "a", Column: "A"}, {Text: "b", Column: "B"}}}
	cases := []struct {
		name  string
		req   contracts.CreateCardRequest
		valid bool
	}{
		{"flip", contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "Q", Answer: contracts.NewStringCardAnswer("A")}, true},
		{"input", contracts.CreateCardRequest{Kind: contracts.CardKindInput, Question: "Q", Answer: contracts.NewStringCardAnswer("A")}, true},
		{"false", contracts.CreateCardRequest{Kind: contracts.CardKindBoolean, Question: "Q", Answer: contracts.NewBoolCardAnswer(false)}, true},
		{"multiple correct choices", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "B", "C"}, Answer: contracts.NewStringArrayCardAnswer([]string{"B", "A"})}, true},
		{"empty correct choices", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "B"}, Answer: contracts.NewStringArrayCardAnswer([]string{})}, false},
		{"duplicate correct choices", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "B"}, Answer: contracts.NewStringArrayCardAnswer([]string{"A", "A"})}, false},
		{"outside correct choices", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "B"}, Answer: contracts.NewStringArrayCardAnswer([]string{"A", "C"})}, false},
		{"choice", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "B"}, Answer: contracts.NewStringCardAnswer("B")}, true},
		{"table", contracts.CreateCardRequest{Kind: contracts.CardKindTable, Question: "Q", Table: contracts.NewOptTableLayout(table), Answer: contracts.NewTableLayoutCardAnswer(table)}, true},
		{"empty question", contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: " ", Answer: contracts.NewStringCardAnswer("A")}, false},
		{"empty answer", contracts.CreateCardRequest{Kind: contracts.CardKindInput, Question: "Q", Answer: contracts.NewStringCardAnswer(" ")}, false},
		{"wrong boolean type", contracts.CreateCardRequest{Kind: contracts.CardKindBoolean, Question: "Q", Answer: contracts.NewStringCardAnswer("false")}, false},
		{"duplicate choices", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{" A ", "A"}, Answer: contracts.NewStringCardAnswer("A")}, false},
		{"answer outside choices", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "B"}, Answer: contracts.NewStringCardAnswer("C")}, false},
		{"missing table", contracts.CreateCardRequest{Kind: contracts.CardKindTable, Question: "Q", Answer: contracts.NewTableLayoutCardAnswer(table)}, false},
		{"unknown kind", contracts.CreateCardRequest{Kind: "unknown", Question: "Q", Answer: contracts.NewStringCardAnswer("A")}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateCard(&tt.req)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v err=%v", tt.valid, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatal(err)
			}
		})
	}
	for _, bad := range []contracts.TableLayout{
		{Columns: []string{"A", "A"}, Items: table.Items},
		{Columns: table.Columns, Items: []contracts.TableItem{{Text: "a", Column: "A"}}},
		{Columns: table.Columns, Items: []contracts.TableItem{{Text: "a", Column: "A"}, {Text: "b", Column: "C"}}},
		{Columns: table.Columns, Items: []contracts.TableItem{{Text: " ", Column: "A"}, {Text: "b", Column: "B"}}},
	} {
		req := contracts.CreateCardRequest{Kind: contracts.CardKindTable, Question: "Q", Table: contracts.NewOptTableLayout(bad), Answer: contracts.NewTableLayoutCardAnswer(bad)}
		if validateCreateCard(&req) == nil {
			t.Fatalf("accepted invalid table: %+v", bad)
		}
	}
	if validateCreateCard(nil) == nil {
		t.Fatal("accepted nil")
	}
}
