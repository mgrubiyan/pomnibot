package usecase

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

// GetSetMistakes tells the author which facts the members of the set get
// wrong. A fact is ranked by how many members made a mistake on it rather
// than by the number of mistakes: a failed card comes back within minutes,
// so one struggling member alone can pile up many of them.
func (s *setServiceImpl) GetSetMistakes(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.SetMistakesResponse, error) {
	pgSetID, err := s.authorSetID(ctx, userID, setID)
	if err != nil {
		return nil, err
	}

	facts, err := s.querier.GetFactsBySetID(ctx, pgSetID)
	if err != nil {
		return nil, fmt.Errorf("get facts by set id: %w", err)
	}
	members, err := s.querier.GetSetStudents(ctx, pgSetID)
	if err != nil {
		return nil, fmt.Errorf("get set students: %w", err)
	}
	counts, err := s.querier.GetSetFactAnswers(ctx, pgSetID)
	if err != nil {
		return nil, fmt.Errorf("get set fact answers: %w", err)
	}

	students := make([]contracts.StudentMistakes, len(members))
	order := make(map[uuid.UUID]int, len(members))
	for i, m := range members {
		students[i] = contracts.StudentMistakes{User: buildUserContract(m.ID, m.FirstName, m.LastName, m.Username)}
		order[m.ID.Bytes] = i
	}

	byFact := make(map[uuid.UUID][]contracts.StudentFactMistakes, len(facts))
	for _, c := range counts {
		// A member who joined between the two queries.
		i, ok := order[c.UserID.Bytes]
		if !ok {
			continue
		}
		students[i].Answers += int(c.Answers)
		students[i].Mistakes += int(c.Mistakes)
		byFact[c.FactID.Bytes] = append(byFact[c.FactID.Bytes], contracts.StudentFactMistakes{
			UserId:   c.UserID.Bytes,
			Answers:  int(c.Answers),
			Mistakes: int(c.Mistakes),
		})
	}

	result := make([]contracts.FactMistakes, len(facts))
	for i, f := range facts {
		entries := byFact[f.ID.Bytes]
		slices.SortFunc(entries, func(a, b contracts.StudentFactMistakes) int {
			return cmp.Compare(order[a.UserId], order[b.UserId])
		})
		fact := contracts.FactMistakes{
			FactId:           f.ID.Bytes,
			FactName:         f.Name,
			StudentsAnswered: len(entries),
			Students:         entries,
		}
		if fact.Students == nil {
			fact.Students = []contracts.StudentFactMistakes{}
		}
		for _, e := range entries {
			fact.Answers += e.Answers
			fact.Mistakes += e.Mistakes
			if e.Mistakes > 0 {
				fact.StudentsMistaken++
			}
		}
		result[i] = fact
	}
	slices.SortStableFunc(result, func(a, b contracts.FactMistakes) int {
		return cmp.Or(
			cmp.Compare(b.StudentsMistaken, a.StudentsMistaken),
			cmp.Compare(b.Mistakes, a.Mistakes),
			cmp.Compare(min(b.Answers, 1), min(a.Answers, 1)),
		)
	})

	return &contracts.SetMistakesResponse{
		SetId:    setID,
		Facts:    result,
		Students: students,
	}, nil
}
