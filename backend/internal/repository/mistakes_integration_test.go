package repository_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// The author's mistakes screen counts the answers of current members only:
// not the author's own, even when the author joined the set too, not those of
// members who left, and not the answers to the cards of other sets.
func TestSetMistakesPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	author := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	bob := uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	carol := uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
	gone := uuid.MustParse("00000000-0000-0000-0000-0000000000d1")

	set := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	other := uuid.MustParse("00000000-0000-0000-0000-000000000102")

	keys := uuid.MustParse("00000000-0000-0000-0000-000000000f01")
	joins := uuid.MustParse("00000000-0000-0000-0000-000000000f02")
	forms := uuid.MustParse("00000000-0000-0000-0000-000000000f03")
	elsewhere := uuid.MustParse("00000000-0000-0000-0000-000000000f04")

	keyFlip := uuid.MustParse("00000000-0000-0000-0000-00000000c001")
	keyChoice := uuid.MustParse("00000000-0000-0000-0000-00000000c002")
	joinFlip := uuid.MustParse("00000000-0000-0000-0000-00000000c003")
	formFlip := uuid.MustParse("00000000-0000-0000-0000-00000000c004")
	elsewhereFlip := uuid.MustParse("00000000-0000-0000-0000-00000000c005")

	for _, sql := range []string{
		fmt.Sprintf(`INSERT INTO users(id,first_name,last_name,data_consent_at) VALUES('%s','Анна','Автор',NULL),('%s','Боб',NULL,NULL),('%s','Карина','Иванова',NOW()),('%s','Ушедший',NULL,NOW())`, author, bob, carol, gone),
		fmt.Sprintf(`INSERT INTO sets(id,title,author_id,share_code) VALUES('%s','БД','%s','123456'),('%s','Другой','%s','234567')`, set, author, other, author),
		// Карина joined after Боб, and Анна joined her own set.
		fmt.Sprintf(`INSERT INTO user_sets(user_id,set_id,joined_at) VALUES
			('%s','%s',NOW() - INTERVAL '2 days'),
			('%s','%s',NOW() - INTERVAL '1 day'),
			('%s','%s',NOW() - INTERVAL '3 days'),
			('%s','%s',NOW())`,
			bob, set, carol, set, author, set, bob, other),
		fmt.Sprintf(`INSERT INTO facts(id,set_id,name) VALUES('%s','%s','Ключи'),('%s','%s','Соединения'),('%s','%s','Нормальные формы'),('%s','%s','Чужой')`,
			keys, set, joins, set, forms, set, elsewhere, other),
		fmt.Sprintf(`INSERT INTO cards(id,fact_id,kind,question,answer_text,explanation,source_quote) VALUES
			('%s','%s','flip','Q','A','E','Q'),
			('%s','%s','choice','Q','A','E','Q'),
			('%s','%s','flip','Q','A','E','Q'),
			('%s','%s','flip','Q','A','E','Q'),
			('%s','%s','flip','Q','A','E','Q')`,
			keyFlip, keys, keyChoice, keys, joinFlip, joins, formFlip, forms, elsewhereFlip, elsewhere),
		// Боб: Ключи 4 answers, 2 wrong, over two cards; Соединения right.
		// Карина: one wrong answer on Ключи. Анна, the gone member and
		// Боб's answer in the other set must not count.
		fmt.Sprintf(`INSERT INTO answers(user_id,card_id,correct) VALUES
			('%[1]s','%[5]s',FALSE),('%[1]s','%[5]s',TRUE),('%[1]s','%[6]s',FALSE),('%[1]s','%[6]s',TRUE),
			('%[1]s','%[7]s',TRUE),
			('%[2]s','%[6]s',FALSE),
			('%[3]s','%[5]s',FALSE),('%[3]s','%[8]s',FALSE),
			('%[4]s','%[5]s',FALSE),('%[4]s','%[8]s',FALSE),
			('%[1]s','%[9]s',FALSE)`,
			bob, carol, author, gone, keyFlip, keyChoice, joinFlip, formFlip, elsewhereFlip),
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}

	q := db.New(pool)
	// The query itself leaves out the author, the gone member and the other
	// set; the service would hide them too, so check the rows directly.
	rows, err := q.GetSetFactAnswers(ctx, pgtype.UUID{Bytes: set, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[[2]uuid.UUID][2]int32{}
	for _, r := range rows {
		got[[2]uuid.UUID{r.FactID.Bytes, r.UserID.Bytes}] = [2]int32{r.Answers, r.Mistakes}
	}
	wantRows := map[[2]uuid.UUID][2]int32{
		{keys, bob}:   {4, 2},
		{joins, bob}:  {1, 0},
		{keys, carol}: {1, 1},
	}
	if len(got) != len(wantRows) {
		t.Fatalf("rows: %v", got)
	}
	for k, v := range wantRows {
		if got[k] != v {
			t.Fatalf("rows: %v, want %v", got, wantRows)
		}
	}

	res, err := usecase.NewSetService(q, usecase.NewUserService(q)).GetSetMistakes(ctx, author, set)
	if err != nil {
		t.Fatal(err)
	}

	// Боб never agreed on the old consent screen; joining the set is enough now.
	if len(res.Students) != 2 {
		t.Fatalf("students: %+v", res.Students)
	}
	shownBob := res.Students[0].User.ID
	if shownBob != bob {
		t.Errorf("student who joined gets a stand-in ID")
	}
	if res.Students[0].Answers != 5 || res.Students[0].Mistakes != 2 ||
		res.Students[1].User.ID != carol || res.Students[1].Answers != 1 || res.Students[1].Mistakes != 1 {
		t.Fatalf("students: %+v", res.Students)
	}
	if last, ok := res.Students[1].User.LastName.Get(); !ok || last != "Иванова" {
		t.Errorf("student name: %+v", res.Students[1].User)
	}
	if u := res.Students[0].User; u.Anonymous.Value || u.FirstName != "Боб" {
		t.Errorf("student who joined is hidden: %+v", u)
	}

	want := []struct {
		name                         string
		answers, mistakes            int
		answered, mistaken, students int
	}{
		{"Ключи", 5, 3, 2, 2, 2},
		{"Соединения", 1, 0, 1, 0, 1},
		{"Нормальные формы", 0, 0, 0, 0, 0},
	}
	if len(res.Facts) != len(want) {
		t.Fatalf("facts: %+v", res.Facts)
	}
	for i, w := range want {
		got := res.Facts[i]
		if got.FactName != w.name || got.Answers != w.answers || got.Mistakes != w.mistakes ||
			got.StudentsAnswered != w.answered || got.StudentsMistaken != w.mistaken || len(got.Students) != w.students {
			t.Errorf("fact %d: %+v want %+v", i, got, w)
		}
	}
	if s := res.Facts[0].Students; s[0].UserId != shownBob || s[0].Answers != 4 || s[0].Mistakes != 2 || s[1].UserId != carol {
		t.Errorf("Ключи by student: %+v", s)
	}

	empty, err := usecase.NewSetService(q, usecase.NewUserService(q)).GetSetMistakes(ctx, author, other)
	if err != nil || len(empty.Students) != 1 || empty.Students[0].Answers != 1 || len(empty.Facts) != 1 {
		t.Fatalf("other set: %+v %v", empty, err)
	}
}
