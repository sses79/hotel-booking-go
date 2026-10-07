// Package dbschema applies the demo's ordered CREATE TABLE/INDEX scripts.
// Oracle DDL commits independently, so existing definitions are checked before
// applying missing objects and every object is checked again after creation.
package dbschema

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type column struct {
	name, dataType, nullable, charUsed string
	length, precision, scale           int
}

type object struct {
	name, kind, table, ddl string
	columns                []column
	keys                   []string
}

var (
	tablePattern  = regexp.MustCompile(`(?s)^CREATE TABLE (\w+)\s*\((.*)\)$`)
	indexPattern  = regexp.MustCompile(`^CREATE UNIQUE INDEX (\w+) ON (\w+)\(([^)]+)\)$`)
	columnPattern = regexp.MustCompile(`^(\w+) (VARCHAR2\((\d+) CHAR\)|NUMBER\((\d+),(\d+)\)|TIMESTAMP\(6\)|DATE|CLOB)( NOT NULL)?$`)
	keyPattern    = regexp.MustCompile(`^PRIMARY KEY \(([^)]+)\)$`)
)

// Apply is restart-safe for the supported additive DDL. It does not alter or
// repair incompatible objects, and is intended to have one schema runner.
func Apply(ctx context.Context, db *sql.DB, directory string) error {
	paths, err := filepath.Glob(filepath.Join(directory, "[0-9]*.sql"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no versioned schema files in %s", directory)
	}
	var objects []object
	seen := make(map[string]bool)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read schema: %w", err)
		}
		parsed, err := parse(string(data))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, obj := range parsed {
			if seen[obj.name] {
				return fmt.Errorf("duplicate schema object %s", obj.name)
			}
			seen[obj.name] = true
			objects = append(objects, obj)
		}
	}
	var missing []object
	for _, obj := range objects {
		var kind string
		err := db.QueryRowContext(ctx,
			"SELECT OBJECT_TYPE FROM USER_OBJECTS WHERE OBJECT_NAME = :1 AND OBJECT_TYPE IN ('TABLE', 'INDEX', 'VIEW', 'SYNONYM', 'SEQUENCE')",
			obj.name).Scan(&kind)
		if err == sql.ErrNoRows {
			missing = append(missing, obj)
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", obj.name, err)
		}
		if kind != obj.kind {
			return fmt.Errorf("incompatible schema object %s: expected %s, found %s", obj.name, obj.kind, kind)
		}
		if err := validate(ctx, db, obj); err != nil {
			return err
		}
	}
	for _, obj := range missing {
		if _, err := db.ExecContext(ctx, obj.ddl); err != nil {
			return fmt.Errorf("create %s: %w", obj.name, err)
		}
		if err := validate(ctx, db, obj); err != nil {
			return err
		}
	}
	return nil
}

func parse(ddl string) ([]object, error) {
	var objects []object
	for _, statement := range strings.Split(ddl, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		upper := strings.ToUpper(statement)
		if m := indexPattern.FindStringSubmatch(upper); m != nil {
			objects = append(objects, object{name: m[1], kind: "INDEX", table: m[2], ddl: statement, keys: names(m[3])})
			continue
		}
		m := tablePattern.FindStringSubmatch(upper)
		if m == nil {
			return nil, fmt.Errorf("unsupported DDL; expected CREATE TABLE or CREATE UNIQUE INDEX")
		}
		obj := object{name: m[1], kind: "TABLE", ddl: statement}
		for _, line := range strings.Split(strings.TrimSpace(m[2]), "\n") {
			line = strings.TrimSuffix(strings.TrimSpace(line), ",")
			if key := keyPattern.FindStringSubmatch(line); key != nil {
				obj.keys = names(key[1])
				continue
			}
			c := columnPattern.FindStringSubmatch(line)
			if c == nil {
				return nil, fmt.Errorf("unsupported column definition in %s", obj.name)
			}
			col := column{name: c[1], dataType: c[2], nullable: "Y", charUsed: "-"}
			if c[6] != "" {
				col.nullable = "N"
			}
			if c[3] != "" {
				col.dataType, col.charUsed = "VARCHAR2", "C"
				col.length, _ = strconv.Atoi(c[3])
			}
			if c[4] != "" {
				col.dataType = "NUMBER"
				col.precision, _ = strconv.Atoi(c[4])
				col.scale, _ = strconv.Atoi(c[5])
			}
			obj.columns = append(obj.columns, col)
		}
		if len(obj.columns) == 0 || len(obj.keys) == 0 {
			return nil, fmt.Errorf("table %s needs columns and a primary key", obj.name)
		}
		objects = append(objects, obj)
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("schema file is empty")
	}
	return objects, nil
}

func names(list string) []string {
	parts := strings.Split(list, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func validate(ctx context.Context, db *sql.DB, obj object) error {
	if obj.kind == "INDEX" {
		var table, uniqueness, status, kind string
		if err := db.QueryRowContext(ctx,
			"SELECT TABLE_NAME, UNIQUENESS, STATUS, INDEX_TYPE FROM USER_INDEXES WHERE INDEX_NAME = :1",
			obj.name).Scan(&table, &uniqueness, &status, &kind); err != nil {
			return fmt.Errorf("inspect index %s: %w", obj.name, err)
		}
		if table != obj.table || uniqueness != "UNIQUE" || status != "VALID" || kind != "NORMAL" {
			return fmt.Errorf("incompatible index %s", obj.name)
		}
		keys, err := readKeys(ctx, db,
			"SELECT COLUMN_NAME FROM USER_IND_COLUMNS WHERE INDEX_NAME = :1 ORDER BY COLUMN_POSITION", obj.name)
		if err != nil {
			return err
		}
		if !slices.Equal(keys, obj.keys) {
			return fmt.Errorf("incompatible index columns for %s", obj.name)
		}
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE, NULLABLE,
		CASE WHEN DATA_TYPE = 'VARCHAR2' THEN CHAR_USED ELSE '-' END,
		CASE WHEN DATA_TYPE = 'VARCHAR2' THEN CHAR_LENGTH ELSE 0 END,
		CASE WHEN DATA_TYPE = 'NUMBER' THEN NVL(DATA_PRECISION, 0) ELSE 0 END,
		CASE WHEN DATA_TYPE = 'NUMBER' THEN NVL(DATA_SCALE, 0) ELSE 0 END
		FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID`, obj.name)
	if err != nil {
		return fmt.Errorf("inspect columns for %s: %w", obj.name, err)
	}
	var actual []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.dataType, &c.nullable, &c.charUsed, &c.length, &c.precision, &c.scale); err != nil {
			rows.Close()
			return err
		}
		actual = append(actual, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !slices.Equal(actual, obj.columns) {
		return fmt.Errorf("incompatible columns for table %s", obj.name)
	}
	keys, err := readKeys(ctx, db, `SELECT CC.COLUMN_NAME
		FROM USER_CONSTRAINTS C JOIN USER_CONS_COLUMNS CC ON C.CONSTRAINT_NAME = CC.CONSTRAINT_NAME
		WHERE C.TABLE_NAME = :1 AND C.CONSTRAINT_TYPE = 'P'
		AND C.STATUS = 'ENABLED' AND C.VALIDATED = 'VALIDATED' AND C.DEFERRABLE = 'NOT DEFERRABLE'
		ORDER BY CC.POSITION`, obj.name)
	if err != nil {
		return err
	}
	if !slices.Equal(keys, obj.keys) {
		return fmt.Errorf("incompatible primary key for table %s", obj.name)
	}
	return nil
}

func readKeys(ctx context.Context, db *sql.DB, query, name string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
