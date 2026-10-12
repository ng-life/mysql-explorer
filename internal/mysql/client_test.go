package mysql

import (
	"strings"
	"testing"
)

func TestSQLReadOnly(t *testing.T) {
	for _, sql := range []string{"SELECT * FROM db.orders", "SELECT a.id, b.name AS customer FROM db.orders a JOIN db.customers b ON a.customer_id=b.id", "SELECT count(*) FROM db.orders", "SELECT 'DELETE FROM x' AS label", "SELECT 1 UNION ALL SELECT 2"} {
		if _, err := ParseSQL(sql); err != nil {
			t.Fatalf("rejected %s: %v", sql, err)
		}
	}
	for _, sql := range []string{"DELETE FROM t", "UPDATE t SET x=1", "SELECT 1; DELETE FROM t", "SELECT * FROM t FOR UPDATE", "SELECT * INTO OUTFILE '/tmp/a' FROM t", "SELECT get_lock('x',1)", "SELECT dangerous_custom_function()", "SELECT sleep(10)", "SELECT * FROM t LOCK IN SHARE MODE"} {
		if _, err := ParseSQL(sql); err == nil {
			t.Fatalf("accepted %s", sql)
		}
	}
}
func TestPagingRespectsUserLimit(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		skip int
		want string
	}{{"SELECT * FROM db.t", 0, "limit 0, 21"}, {"SELECT * FROM db.t LIMIT 25 OFFSET 10", 20, "limit 30, 5"}, {"SELECT * FROM db.t LIMIT 5", 20, "limit 20, 0"}} {
		node, err := ParseSQL(tc.sql)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pageSQL(node, Query{Limit: 20, Skip: tc.skip})
		if err != nil || !strings.Contains(out, tc.want) {
			t.Fatalf("%s %v", out, err)
		}
	}
	if quoteIdentifier("a`;DELETE") != "`a``;DELETE`" {
		t.Fatal("bad identifier escaping")
	}
}
