//go:build perf

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const ownerExternalID = 9000000

func setID(n int) string           { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }
func cardID(fact, kind int) string { return fmt.Sprintf("00000000-0000-0000-0001-%012d", fact*5+kind) }

// Fixture identities and historical timestamps are deterministic.
// Delete shipped demo data only inside the newly created container.
func seed(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []string{
		`TRUNCATE users, topics CASCADE`,
		`INSERT INTO users(id,max_id,first_name,last_name) VALUES(9000000,9000000,'Teacher','Perf')`,
		`INSERT INTO users(id,first_name,last_name) SELECT i,'Background',i::text FROM generate_series(1,1000) i`,
		`INSERT INTO sets(id,title,author_id,share_code) SELECT ('00000000-0000-0000-0000-'||lpad(i::text,12,'0'))::uuid,'Perf '||i,9000000,(700000+i)::text FROM generate_series(1,3) i`,
		`INSERT INTO user_sets(user_id,set_id) SELECT u.id,s.id FROM users u CROSS JOIN sets s`,
		`INSERT INTO facts(id,set_id,name) SELECT i::text,('00000000-0000-0000-0000-'||lpad(((i-1)/50+1)::text,12,'0'))::uuid,'Fact '||i FROM generate_series(1,150) i`,
		`INSERT INTO cards(id,fact_id,kind,question,answer_text,explanation,source_quote)
 SELECT ('00000000-0000-0000-0001-'||lpad((i*5+k)::text,12,'0'))::uuid,i::text,
 (ARRAY['flip','boolean','choice','table','input'])[k+1], 'Question '||i,
 CASE WHEN k=1 THEN 'true' ELSE 'Answer' END,'Explanation','Source' FROM generate_series(1,150) i CROSS JOIN generate_series(0,4) k`,
		`INSERT INTO card_options(card_id,position,text,is_correct) SELECT id,p,CASE WHEN p=0 THEN 'Answer' ELSE 'Other' END,p=0 FROM cards CROSS JOIN generate_series(0,1) p WHERE kind='choice'`,
		`INSERT INTO card_table_columns(card_id,position,name) SELECT id,p,CASE WHEN p=0 THEN 'A' ELSE 'B' END FROM cards CROSS JOIN generate_series(0,1) p WHERE kind='table'`,
		`INSERT INTO card_table_items(card_id,item_text,column_name) SELECT id,'Item','A' FROM cards WHERE kind='table'`,
		`INSERT INTO user_fact_progress(user_id,fact_id,elo,next_review_at) SELECT u.id,f.id,800,'2020-01-01' FROM users u CROSS JOIN facts f`,
		`INSERT INTO answers(id,user_id,card_id,correct)
 SELECT ('00000000-0000-0000-0002-'||lpad(i::text,12,'0'))::uuid, (i-1)%1000+1,
 ('00000000-0000-0000-0001-'||lpad((((i-1)%150+1)*5+(i-1)%5)::text,12,'0'))::uuid,
 i%4<>0 FROM generate_series(1,100000) i`,
		`ANALYZE`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("fixture: %w", err)
		}
	}
	return nil
}
