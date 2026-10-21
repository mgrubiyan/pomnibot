package generator

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

func objectivePriority(o Objective) int {
	switch o.Priority {
	case "detail":
		return 2
	case "supporting":
		return 1
	default:
		return 0
	}
}
func repairTasks(p Plan, covered map[string]bool, d material.Document, limit int) []Topic {
	if limit < 0 {
		return nil
	}
	var tasks []Topic
	for _, t := range p.Topics {
		goals := slices.DeleteFunc(slices.Clone(t.Objectives), func(o Objective) bool { return covered[o.ID] })
		if len(goals) == 0 {
			continue
		}
		originalGoals := t.Objectives
		t.Objectives = goals
		// Keep all definitions/conditions and the full blocks for missing goals,
		// leaving unrelated examples out of a repair.
		var cs []reviewCandidate
		for _, o := range goals {
			for _, id := range o.BlockIDs {
				cs = append(cs, reviewCandidate{Evidence: []material.Evidence{{BlockID: id}}})
			}
		}
		task := reviewContext(t, d, cs)
		// Source context with no objective of its own can still define the rule.
		for _, id := range topicBlockIDs(t) {
			assigned := slices.ContainsFunc(originalGoals, func(o Objective) bool { return slices.Contains(o.BlockIDs, id) })
			if !assigned && !slices.Contains(task.BlockIDs, id) {
				task.BlockIDs = append(task.BlockIDs, id)
			}
		}
		tasks = append(tasks, task)
	}
	slices.SortStableFunc(tasks, func(a, b Topic) int {
		priority := func(t Topic) int {
			n := 2
			for _, o := range t.Objectives {
				n = min(n, objectivePriority(o))
			}
			return n
		}
		if diff := priority(a) - priority(b); diff != 0 {
			return diff
		}
		return len(b.Objectives) - len(a.Objectives)
	})
	return tasks[:min(len(tasks), limit)]
}

// Response volume matters as well as card count: large code/table questions
// receive smaller batches, and small candidates share one source transmission.
func reviewBatches(cs []cards.Card, maxCards, sourceBudget int) [][]cards.Card {
	var out [][]cards.Card
	start, size := 0, 0
	for i, c := range cs {
		raw, _ := json.Marshal(c)
		if i > start && (i-start >= maxCards || size+len(raw) > sourceBudget/2) {
			out = append(out, cs[start:i])
			start = i
			size = 0
		}
		size += len(raw)
	}
	if start < len(cs) {
		out = append(out, cs[start:])
	}
	return out
}

// Scope may be introduced once for several rules on distant pages. Preserve
// its exact original block instead of requiring the rule to repeat the name.
func bindObjectiveScopes(p *Plan, d material.Document) {
	for i := range p.Topics {
		t := &p.Topics[i]
		for j := range t.Objectives {
			o := &t.Objectives[j]
			for _, scope := range o.Scope {
				for _, id := range topicBlockIDs(*t) {
					b, ok := d.Block(id)
					if ok && strings.Contains(b.Content(), scope) && !slices.Contains(o.BlockIDs, id) {
						o.BlockIDs = append(o.BlockIDs, id)
						break
					}
				}
			}
		}
	}
}

// Repeated page warnings and layout coordinates add no new planning evidence.
// Keep each warning once per page; full source artifacts remain untouched.
func planningBlocks(d material.Document) []material.Block {
	out := slices.Clone(d.Blocks)
	seen := map[int]map[string]bool{}
	for i := range out {
		b := &out[i]
		b.Region = nil
		b.Normalized = ""
		b.Order = 0
		if seen[b.Page] == nil {
			seen[b.Page] = map[string]bool{}
		}
		var issues []string
		for _, issue := range b.Issues {
			if !seen[b.Page][issue] {
				issues = append(issues, issue)
				seen[b.Page][issue] = true
			}
		}
		b.Issues = issues
	}
	return out
}
