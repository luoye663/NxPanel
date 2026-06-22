// db 包的手动 Schema 导出工具测试。
//
// 默认跳过，避免污染日常测试输出。需要重新生成首版基线时运行：
//
//	NXPANEL_DUMP_SCHEMA=1 go test ./internal/db -run TestDumpCurrentSchemaForBaseline -count=1 -v
package db

import (
	"fmt"
	"os"
	"testing"
)

func TestDumpCurrentSchemaForBaseline(t *testing.T) {
	if os.Getenv("NXPANEL_DUMP_SCHEMA") != "1" {
		t.Skip("set NXPANEL_DUMP_SCHEMA=1 to dump the current SQLite schema")
	}

	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`
		SELECT type, name, sql
		FROM sqlite_schema
		WHERE sql IS NOT NULL
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY CASE type
		  WHEN 'table' THEN 1
		  WHEN 'index' THEN 2
		  WHEN 'trigger' THEN 3
		  WHEN 'view' THEN 4
		  ELSE 5
		END, name
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("--SCHEMA-START--")
	for rows.Next() {
		var schemaType, name, sql string
		if err := rows.Scan(&schemaType, &name, &sql); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s;\n\n", sql)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	seedRows, err := database.Query(`
		SELECT
		  quote(id), quote(name), quote(category), quote(description), quote(params_json),
		  quote(template), enabled, sort_order, quote(created_at), quote(updated_at)
		FROM rewrite_templates
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer seedRows.Close()

	fmt.Println("--REWRITE-TEMPLATES-SEED-START--")
	for seedRows.Next() {
		var id, name, category, description, paramsJSON, template, createdAt, updatedAt string
		var enabled, sortOrder int
		if err := seedRows.Scan(&id, &name, &category, &description, &paramsJSON, &template, &enabled, &sortOrder, &createdAt, &updatedAt); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("(%s, %s, %s, %s, %s, %s, %d, %d, %s, %s),\n", id, name, category, description, paramsJSON, template, enabled, sortOrder, createdAt, updatedAt)
	}
	if err := seedRows.Err(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("--DUMP-END--")
}
