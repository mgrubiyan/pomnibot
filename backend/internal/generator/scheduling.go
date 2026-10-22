package generator

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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
		goals := slices.DeleteFunc(slices.Clone(t.Objectives), func(o Objective) bool { return covered[o.ID] || o.BlockedReason != "" })
		if len(goals) == 0 {
			continue
		}
		task := t
		task.Objectives = goals
		task.BlockIDs = nil
		task.RelatedBlockIDs = nil
		for _, o := range goals {
			sub := objectiveTask(t, o, d)
			for _, id := range sub.BlockIDs {
				if !slices.Contains(task.BlockIDs, id) {
					task.BlockIDs = append(task.BlockIDs, id)
				}
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
	// Page headings provide local context even when the model assigns only the
	// rule/example blocks to a topic. Never borrow headings from another page
	// or from a later section, or treat visual interpretations as source text.
	headings := map[string][]material.Block{}
	var stack []material.Block
	page := 0
	for _, b := range d.Blocks {
		if b.Page != page {
			stack = nil
			page = b.Page
		}
		if b.Kind == "heading" {
			for len(stack) > 0 {
				parent := stack[len(stack)-1]
				// Only visible, strictly smaller subheadings establish nesting.
				// Without measured regions retain the conservative nearest-heading rule.
				if b.Region != nil && parent.Region != nil && material.ValidRegion(b.Region) && material.ValidRegion(parent.Region) &&
					b.Region[1] >= parent.Region[3] && b.Region[3]-b.Region[1] < .9*(parent.Region[3]-parent.Region[1]) {
					break
				}
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, b)
		}
		if b.Page > 0 {
			headings[b.ID] = slices.Clone(stack)
		}
	}
	for i := range p.Topics {
		t := &p.Topics[i]
		for j := range t.Objectives {
			o := &t.Objectives[j]
			// Column/page headings can state necessary conditions even when
			// there is no named dialect/scope (for example, without an index).
			for _, id := range slices.Clone(o.BlockIDs) {
				for _, h := range headings[id] {
					if !slices.Contains(o.BlockIDs, h.ID) {
						o.BlockIDs = append(o.BlockIDs, h.ID)
					}
					if !slices.Contains(t.BlockIDs, h.ID) {
						t.BlockIDs = append(t.BlockIDs, h.ID)
					}
				}
			}
			// Native slide captions can be ordered after the formulas in OCR.
			// Bind their literal qualifiers to the same local source section;
			// this adds evidence, never a new or easier learning objective.
			for _, id := range slices.Clone(o.BlockIDs) {
				anchor, ok := d.Block(id)
				if !ok || anchor.Page <= 0 {
					continue
				}
				for _, caption := range d.Blocks {
					if caption.Page != anchor.Page || !isContextCaption(caption) {
						continue
					}
					local := headings[caption.ID]
					anchorHeadings := headings[id]
					if len(local) > 0 && (len(anchorHeadings) == 0 || local[len(local)-1].ID != anchorHeadings[len(anchorHeadings)-1].ID) {
						continue
					}
					if len(local) == 0 && len(headings[id]) > 0 {
						continue
					}
					o.BlockIDs = appendUnique(o.BlockIDs, caption.ID)
					t.BlockIDs = appendUnique(t.BlockIDs, caption.ID)
				}
			}
			for _, scope := range o.Scope {
				var source string
				for _, id := range topicBlockIDs(*t) {
					b, ok := d.Block(id)
					if ok && containsScope(b.Content(), scope) {
						source = id
						break
					}
				}
				if source == "" {
					for _, id := range o.BlockIDs {
						for _, h := range headings[id] {
							if containsScope(h.Text, scope) {
								source = h.ID
								break
							}
						}
						if source != "" {
							break
						}
					}
				}
				if source != "" {
					if !slices.Contains(o.BlockIDs, source) {
						o.BlockIDs = append(o.BlockIDs, source)
					}
					if !slices.Contains(t.BlockIDs, source) {
						t.BlockIDs = append(t.BlockIDs, source)
					}
				}
			}
		}
	}
}

// Scope names are case insensitive, but must not match part of another name
// (for example, "плотный индекс" inside "неплотный индекс"). Quotes remain exact.
var scopeHyphenSpacing = regexp.MustCompile(`([\p{L}\p{N}])\s*[-–—]\s*([\p{L}\p{N}])`)

func containsScope(text, scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return false
	}
	// Typography does not create a different technical name. Evidence quotes
	// remain exact; only this scope-name comparison normalizes hyphen spacing.
	text = scopeHyphenSpacing.ReplaceAllString(strings.ToLower(text), "$1-$2")
	scope = scopeHyphenSpacing.ReplaceAllString(strings.ToLower(scope), "$1-$2")
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }
	for offset := 0; offset <= len(text)-len(scope); {
		index := strings.Index(text[offset:], scope)
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(scope)
		before, _ := utf8.DecodeLastRuneInString(text[:index])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if (index == 0 || !word(before)) && (end == len(text) || !word(after)) {
			return true
		}
		offset = end
	}
	return false
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

// Each goal gets at most two repair generations, including empty/duplicate
// responses. Splitting precedes the shared document limit, so a large topic
// cannot consume an unbounded number of calls under a single repair slot.
func nextRepairTasks(p Plan, covered map[string]bool, d material.Document, limit, budget int, attempts map[string]int) []Topic {
	if limit <= 0 {
		return nil
	}
	unavailable := map[string]bool{}
	for _, t := range p.Topics {
		for _, o := range t.Objectives {
			unavailable[o.ID] = covered[o.ID] || attempts[o.ID] >= 2
		}
	}
	candidates := repairTasks(p, unavailable, d, len(p.Topics))
	tasks := splitTopicTasks(Plan{Topics: candidates}, d, budget)
	tasks = tasks[:min(limit, len(tasks))]
	for _, t := range tasks {
		for _, o := range t.Objectives {
			attempts[o.ID]++
		}
	}
	return tasks
}

// Questions may inflect a Russian source label. Source grounding stays literal
// (containsScope); only this presence check permits grammatical endings. Whole
// consecutive words and their prefixes remain distinct, including negation.
func questionContainsScope(question, scope string) bool {
	if containsScope(question, scope) {
		return true
	}
	words := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '_' })
	}
	q, label := words(question), words(scope)
	if len(label) == 0 {
		return false
	}
	for i := 0; i+len(label) <= len(q); i++ {
		match := true
		for j, w := range label {
			if russianScopeStem(w) != russianScopeStem(q[i+j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func russianScopeStem(word string) string {
	// Technical identifiers and mixed-script names must still match exactly.
	for _, r := range word {
		if r != '-' && (r < 'а' || r > 'я') && r != 'ё' {
			return word
		}
	}
	for _, suffix := range []string{"ыми", "ими", "ого", "его", "ему", "ому", "ами", "ями", "ая", "яя", "ую", "юю", "ое", "ее", "ые", "ие", "ый", "ий", "ой", "ым", "им", "ом", "ем", "ых", "их", "ов", "ев", "ам", "ям", "ах", "ях", "а", "я", "у", "ю", "е", "и", "ы", "о"} {
		if strings.HasSuffix(word, suffix) {
			stem := strings.TrimSuffix(word, suffix)
			if utf8.RuneCountInString(stem) >= 4 {
				return stem
			}
		}
	}
	return word
}

// The source and complete audit history remain untouched. Requests carry recent
// actionable reasons and accepted questions, without repeated evidence quotes.
func repairFeedback(topicID string, reviews []ReviewRecord, budget int) string {
	feedback := []ReviewRecord{}
	for _, accepted := range []bool{true, false} {
		for i := len(reviews) - 1; i >= 0; i-- {
			v := reviews[i]
			if v.TopicID != topicID || v.Accept != accepted {
				continue
			}
			v.Evidence = nil
			v.Candidate = nil
			v.PriorProblem = ""
			reason := []rune(v.Problem)
			if len(reason) > 400 {
				v.Problem = string(reason[:400]) + "…"
			}
			proposed := append(slices.Clone(feedback), v)
			raw, _ := json.Marshal(proposed)
			allowance := budget
			if accepted {
				allowance = budget / 2
			}
			if len(raw) <= allowance {
				feedback = proposed
			}
		}
	}
	raw, _ := json.Marshal(feedback)
	return string(raw)
}

func isContextCaption(b material.Block) bool {
	text := strings.TrimSpace(b.Text)
	return b.Kind == "text" && utf8.RuneCountInString(text) <= 320 && strings.Contains(text, "\n(") && strings.HasSuffix(text, ")")
}
