package dbschema

import (
	"os"
	"testing"
)

func TestInitialSchemaDefinitions(t *testing.T) {
	data, err := os.ReadFile("../../schema/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := parse(string(data))
	if err != nil || len(objects) != 9 {
		t.Fatalf("objects=%d err=%v", len(objects), err)
	}
	if objects[0].columns[1].length != 200 || objects[0].columns[1].charUsed != "C" || objects[0].columns[2].dataType != "TIMESTAMP(6)" {
		t.Fatal("incorrect hotel column metadata")
	}
	if objects[7].columns[2].dataType != "CLOB" || objects[7].columns[4].nullable != "Y" {
		t.Fatal("incorrect outbox metadata")
	}
}

func TestUnsupportedSchemaFailsBeforeExecution(t *testing.T) {
	for _, ddl := range []string{"", "DROP TABLE Hotels", "CREATE TABLE Bad (\nID VARCHAR2(36 CHAR)\n)", "CREATE TABLE Bad (\nID BOGUS NOT NULL,\nPRIMARY KEY (ID)\n)"} {
		if _, err := parse(ddl); err == nil {
			t.Fatalf("accepted unsupported schema %q", ddl)
		}
	}
}
