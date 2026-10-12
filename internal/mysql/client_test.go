package mysql

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParameterizedQuery(t *testing.T) {
	cols := []Column{{Name: "id", Key: "PRI"}, {Name: "name"}, {Name: "deleted"}}
	sql, args, err := buildQuery("db`name", "orders", cols, Query{Filter: map[string]any{"name": "'; DROP TABLE orders; --", "deleted": nil}, Sort: map[string]int{"name": -1}, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	expected := "SELECT * FROM `db``name`.`orders` WHERE `deleted` IS NULL AND `name` = ? ORDER BY `name` DESC, `id` ASC LIMIT ? OFFSET ?"
	if sql != expected || !reflect.DeepEqual(args, []any{"'; DROP TABLE orders; --", 21, 0}) {
		t.Fatalf("unexpected query: %s, %v", sql, args)
	}
}
func TestRejectInvalidQueries(t *testing.T) {
	cols := []Column{{Name: "id", Key: "PRI"}}
	cases := []Query{
		{Limit: 201}, {Limit: 20, Skip: -1}, {Limit: 20, Filter: map[string]any{"id` OR 1=1": 1}},
		{Limit: 20, Sort: map[string]int{"id": 2}}, {Limit: 20, Filter: map[string]any{"id": map[string]any{"$gt": 1}}},
	}
	for _, q := range cases {
		if _, _, err := buildQuery("db", "t", cols, q); err == nil {
			t.Fatalf("accepted invalid query: %+v", q)
		}
	}
}
func TestLargeNumberStaysExact(t *testing.T) {
	_, args, err := buildQuery("db", "t", []Column{{Name: "id"}}, Query{Limit: 1, Filter: map[string]any{"id": json.Number("9007199254740993")}})
	if err != nil || args[0] != "9007199254740993" {
		t.Fatalf("precision lost: %v, %v", args, err)
	}
	if quoteIdentifier("a`;DELETE") != "`a``;DELETE`" {
		t.Fatal("identifier not escaped")
	}
}
