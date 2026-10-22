package generator

import (
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// Objective preserves a learning goal from historical private reports.
// The associated planning/review algorithms have been removed.
type Objective struct {
	BlockedReason string   `json:"blockedReason,omitempty"` // source validation failure; never scheduled, still a reported gap
	Priority      string   `json:"priority,omitempty"`
	Modes         []string `json:"modes,omitempty"`
	Scope         []string `json:"scope,omitempty"`
	ID            string   `json:"id"`
	Text          string   `json:"text"`
	BlockIDs      []string `json:"blockIds"`
}

// Topic groups related blocks, including nonadjacent pages.
type Topic struct {
	ImageBlockIDs   []string    `json:"imageBlockIds,omitempty"`
	RelatedBlockIDs []string    `json:"relatedBlockIds,omitempty"`
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	Section         string      `json:"section"`
	BlockIDs        []string    `json:"blockIds"`
	Objectives      []Objective `json:"objectives"`
}

// Exclusion records a source block intentionally left out of the learning plan.
// It remains in the private report for auditing; it never counts as coverage.
type Exclusion struct {
	BlockID string `json:"blockId"`
	Reason  string `json:"reason"`
	Kind    string `json:"kind"` // service, duplicate, insufficient
}

// Plan is the document-wide thematic map.
type Plan struct {
	Excluded []Exclusion `json:"excluded,omitempty"`
	Issues   []string    `json:"issues,omitempty"`

	Topics []Topic `json:"topics"`
}

// Coverage records surviving cards against the planned goals.
type Coverage struct {
	Covered    []string `json:"covered"`
	Missing    []string `json:"missing"`
	Issues     []string `json:"issues,omitempty"`
	Unassigned []string `json:"unassigned,omitempty"`
}

// ReviewRecord preserves a fallible verifier verdict and its evidence.
type ReviewRecord struct {
	Candidate    *reviewCandidate    `json:"candidate,omitempty"`
	Defect       string              `json:"defect,omitempty"`
	DefectQuote  string              `json:"defectQuote,omitempty"`
	TopicID      string              `json:"topicId"`
	Question     string              `json:"question"`
	Accept       bool                `json:"accept"`
	Problem      string              `json:"problem"`
	Evidence     []material.Evidence `json:"evidence"`
	Covered      []string            `json:"covered"`
	PriorProblem string              `json:"priorProblem,omitempty"`
	Rechecked    bool                `json:"rechecked,omitempty"`
	Audited      bool                `json:"audited,omitempty"`
}

type reviewCandidate struct {
	LearningMode string              `json:"learningMode,omitempty"`
	Index        int                 `json:"index"`
	Question     string              `json:"question"`
	Answer       string              `json:"answer"`
	Kind         cards.Kind          `json:"kind"`
	Options      []string            `json:"options,omitempty"`
	Explanation  string              `json:"explanation"`
	Evidence     []material.Evidence `json:"evidence"`
	Table        *cards.Table        `json:"table,omitempty"`
}
