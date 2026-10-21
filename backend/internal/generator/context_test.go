package generator

import (
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"slices"
	"strings"
	"testing"
)

func TestRelatedCodeKeepsDistantExampleWithFormalRule(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "example", Page: 22, Kind: "code", Text: "FOREIGN KEY (x) REFERENCES P(id) ON DELETE CASCADE"}, {ID: "rule", Page: 33, Text: "ON DELETE CASCADE удаляет связанные дочерние строки."}, {ID: "other", Page: 27, Kind: "code", Text: "INSERT INTO T VALUES (1)"}}}
	p := Plan{Topics: []Topic{{ID: "actions", BlockIDs: []string{"rule"}}}}
	relatedCode(&p, d)
	if !slices.Contains(p.Topics[0].RelatedBlockIDs, "example") || slices.Contains(p.Topics[0].RelatedBlockIDs, "other") {
		t.Fatal("distant source context lost or unrelated example added")
	}
	source, err := topicSource(p.Topics[0], d, 24000)
	if err != nil || !strings.Contains(source, "FOREIGN KEY") {
		t.Fatal("related code did not reach generation")
	}
}
