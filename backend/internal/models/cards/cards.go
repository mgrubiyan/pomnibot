// Package cards holds the storage and feed models, plus exact choice answer comparison.
package cards

import "github.com/mgrubiyan/pomnibot/backend/internal/material"

// Kind is the card type. Mirrors CardKind in miniapp/src/types.ts.
type Kind string

// Card kinds.
const (
	KindChoice  Kind = "choice"
	KindBoolean Kind = "boolean"
	KindFlip    Kind = "flip"
	KindInput   Kind = "input"
	// KindTable is a sorting card: the student lays terms out across the
	// columns of Table.
	KindTable Kind = "table"
)

// Boolean answers, as the frontend expects them.
const (
	AnswerTrue  = "true"
	AnswerFalse = "false"
)

// Fact is one statement of the notes. A card refers to its fact by id.
type Fact struct {
	ID    string `json:"id"`   // UUID assigned by the usecase
	Name  string `json:"name"` // short name of the fact, narrower than Topic
	Topic string `json:"topic"`
}

// Card mirrors Card in miniapp/src/types.ts without id and setId, which the
// storage assigns, and topic, which its fact holds; plus FactID.
type Card struct {
	Evidence     []material.Evidence `json:"evidence,omitempty"`
	FactID       string              `json:"factId"`
	Kind         Kind                `json:"kind"`
	Question     string              `json:"question"`
	RightAnswers []string            `json:"rightAnswers,omitempty"`
	Options      []string            `json:"options,omitempty"` // choice only, includes all RightAnswers
	Answer       string              `json:"answer"`            // boolean text; JSON string array for multiple choice answers
	Explanation  string              `json:"explanation"`
	SourceQuote  string              `json:"sourceQuote"` // exact text of the normalized notes
	SourceRef    string              `json:"sourceRef,omitempty"`
	Table        *Table              `json:"table,omitempty"` // table only; Answer is empty
}

// Table mirrors TableLayout in miniapp/src/types.ts: terms and the column
// each belongs to. The frontend shuffles Items before showing the card.
type Table struct {
	Columns []string    `json:"columns"`
	Items   []TableItem `json:"items"`
}

// TableItem is a term of a table and its column, one of Table.Columns.
type TableItem struct {
	Text   string `json:"text"`
	Column string `json:"column"`
}
