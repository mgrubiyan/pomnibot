// Package cards holds the card model: what the generator produces, the
// storage keeps and the feed shows. No logic lives here.
package cards

// Kind is the card type. Mirrors CardKind in miniapp/src/types.ts, without
// table cards: those need a layout the model cannot ground in one quote.
type Kind string

// Card kinds.
const (
	KindChoice  Kind = "choice"
	KindBoolean Kind = "boolean"
	KindFlip    Kind = "flip"
	KindInput   Kind = "input"
)

// Boolean answers, as the frontend expects them.
const (
	AnswerTrue  = "true"
	AnswerFalse = "false"
)

// Card mirrors Card in miniapp/src/types.ts without id and setId, which the
// storage assigns, plus FactID.
//
// Cards with the same FactID test one fact in different forms (choice, input,
// boolean, flip) and share the quote and FactName. The feed is meant to show
// one of them at a time and rotate the form between repetitions.
type Card struct {
	FactID      string   `json:"factId"`   // derived from the document id and the quote
	FactName    string   `json:"factName"` // short name of the fact, narrower than Topic
	Kind        Kind     `json:"kind"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"` // choice only, includes Answer
	Answer      string   `json:"answer"`            // "true" | "false" for boolean
	Explanation string   `json:"explanation"`
	SourceQuote string   `json:"sourceQuote"` // exact text of the normalized notes
	SourceRef   string   `json:"sourceRef,omitempty"`
	Topic       string   `json:"topic"`
}
