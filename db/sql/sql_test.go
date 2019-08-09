// Copyright (C) 2015 NTT Innovation Institute, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sql_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/cloudwan/gohan/db"
	"github.com/cloudwan/gohan/db/dbutil"
	"github.com/cloudwan/gohan/db/initializer"
	"github.com/cloudwan/gohan/db/options"
	"github.com/cloudwan/gohan/db/pagination"
	"github.com/cloudwan/gohan/db/search"
	. "github.com/cloudwan/gohan/db/sql"
	"github.com/cloudwan/gohan/db/transaction"
	"github.com/cloudwan/gohan/schema"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sql", func() {

	const testFixtures = "test_fixture.json"

	var (
		conn       string
		tx         transaction.Transaction
		sqlConn    *DB
		testSchema *schema.Schema
		ctx        context.Context
	)

	BeforeEach(func() {
		var dbType string
		if os.Getenv("MYSQL_TEST") == "true" {
			conn = "root@tcp(localhost:3306)/gohan_test"
			dbType = "mysql"
		} else {
			conn = "./test.db"
			dbType = "sqlite3"
		}

		ctx = context.Background()

		manager := schema.GetManager()
		dbc, err := dbutil.ConnectDB(dbType, conn, db.DefaultMaxOpenConn, options.Default())
		sqlConn = dbc.(*DB)
		Expect(err).ToNot(HaveOccurred())
		Expect(manager.LoadSchemasFromFiles(
			"../../etc/schema/gohan.json", "../../tests/test_abstract_schema.yaml", "../../tests/test_schema.yaml")).To(Succeed())
		dbutil.InitDBWithSchemas(dbType, conn, db.DefaultTestInitDBParams())

		source, err := initializer.NewInitializer(testFixtures)
		Expect(err).ToNot(HaveOccurred())

		Expect(dbutil.CopyDBResources(source, dbc, true)).To(Succeed())

		tx, err = dbc.BeginTx()
		Expect(err).ToNot(HaveOccurred())

		var ok bool
		testSchema, ok = manager.Schema("test")
		Expect(ok).To(BeTrue())
		Expect(testSchema).ToNot(BeNil())
	})

	AfterEach(func() {
		tx.Close()
		sqlConn.Close()

		schema.ClearManager()
		if os.Getenv("MYSQL_TEST") != "true" {
			os.Remove(conn)
		}
	})

	Describe("Select Pagination", func() {
		var s *schema.Schema
		var totalBefore uint64

		BeforeEach(func() {
			manager := schema.GetManager()
			var ok bool
			s, ok = manager.Schema("test")
			Expect(ok).To(BeTrue())

			var err error
			totalBefore, err = tx.Count(ctx, s, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		insertTwoRecords := func() {
			Expect(tx.Exec(ctx, "INSERT INTO `tests` (`id`, `tenant_id`, `domain_id`) values ('id1', 'tenant1', 'domain')")).To(Succeed())
			Expect(tx.Exec(ctx, "INSERT INTO `tests` (`id`, `tenant_id`, `domain_id`) values ('id2', 'tenant2', 'domain')")).To(Succeed())
		}

		listWithPaginator := func(pg *pagination.Paginator) ([]*schema.Resource, uint64) {
			results, total, err := tx.List(ctx, s, map[string]interface{}{}, nil, pg)
			Expect(err).To(Succeed())
			return results, total
		}

		It("Empty key doesn't exclude limit/offset pagination", func() {
			insertTwoRecords()

			pg, err := pagination.NewPaginator(pagination.OptionLimit(1))
			Expect(err).To(Succeed())
			results, total := listWithPaginator(pg)

			Expect(len(results)).To(Equal(1))
			Expect(total).To(Equal(totalBefore + 2))
		})

		It("If Limit is set to 0, total is still returned correctly", func() {
			insertTwoRecords()

			pg, err := pagination.NewPaginator(pagination.OptionLimit(0))
			Expect(err).To(Succeed())
			results, total := listWithPaginator(pg)

			Expect(results).To(BeEmpty())
			Expect(total).To(Equal(totalBefore + 2))
		})
	})

	Describe("MakeColumns", func() {
		var s *schema.Schema

		BeforeEach(func() {
			manager := schema.GetManager()
			var ok bool
			s, ok = manager.Schema("test")
			Expect(ok).To(BeTrue())
		})

		Context("Without fields", func() {
			It("Returns all colums", func() {
				cols := MakeColumns(s, s.GetDbTableName(), nil, false)
				Expect(cols).To(HaveLen(7))
			})
		})

		Context("With fields", func() {
			It("Returns selected colums", func() {
				cols := MakeColumns(s, s.GetDbTableName(), []string{"test.id", "test.tenant_id"}, false)
				Expect(cols).To(HaveLen(2))
			})
		})
	})

	Describe("Query", func() {
		type testRow struct {
			ID          string  `json:"id"`
			TenantID    string  `json:"tenant_id"`
			DomainID    string  `json:"domain_id"`
			TestString  string  `json:"test_string"`
			TestNumber  float64 `json:"test_number"`
			TestInteger int     `json:"test_integer"`
			TestBool    bool    `json:"test_bool"`
		}

		var (
			s        *schema.Schema
			expected []*testRow
		)

		var v map[string][]*testRow
		readFixtures(testFixtures, &v)
		expected = v["tests"]

		BeforeEach(func() {
			manager := schema.GetManager()
			var ok bool
			s, ok = manager.Schema("test")
			Expect(ok).To(BeTrue())
		})

		Context("Without place holders", func() {
			It("Returns resources", func() {
				query := fmt.Sprintf(
					"SELECT %s FROM %s",
					strings.Join(MakeColumns(s, s.GetDbTableName(), nil, false), ", "),
					s.GetDbTableName(),
				)
				results, err := tx.Query(ctx, s, query, []interface{}{})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(results)).To(Equal(4))

				for i, r := range results {
					Expect(r.Data()).To(Equal(map[string]interface{}{
						"id":           expected[i].ID,
						"tenant_id":    expected[i].TenantID,
						"domain_id":    expected[i].DomainID,
						"test_string":  expected[i].TestString,
						"test_number":  expected[i].TestNumber,
						"test_integer": expected[i].TestInteger,
						"test_bool":    expected[i].TestBool,
					}))
				}
			})
		})

		Context("With a place holder", func() {
			It("Replace the place holder and returns resources", func() {
				query := fmt.Sprintf(
					"SELECT %s FROM %s WHERE tenant_id = ?",
					strings.Join(MakeColumns(s, s.GetDbTableName(), nil, false), ", "),
					s.GetDbTableName(),
				)
				results, err := tx.Query(ctx, s, query, []interface{}{"tenant0"})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(results)).To(Equal(2))

				for i, r := range results {
					Expect(r.Data()).To(Equal(map[string]interface{}{
						"id":           expected[i].ID,
						"tenant_id":    expected[i].TenantID,
						"domain_id":    expected[i].DomainID,
						"test_string":  expected[i].TestString,
						"test_number":  expected[i].TestNumber,
						"test_integer": expected[i].TestInteger,
						"test_bool":    expected[i].TestBool,
					}))
				}
			})
		})

		Context("With place holders", func() {
			It("Replace the place holders and returns resources", func() {
				query := fmt.Sprintf(
					"SELECT %s FROM %s WHERE tenant_id = ? AND test_string = ?",
					strings.Join(MakeColumns(s, s.GetDbTableName(), nil, false), ", "),
					s.GetDbTableName(),
				)
				results, err := tx.Query(ctx, s, query, []interface{}{"tenant0", "obj1"})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(results)).To(Equal(1))

				Expect(results[0].Data()).To(Equal(map[string]interface{}{
					"id":           expected[1].ID,
					"tenant_id":    expected[1].TenantID,
					"domain_id":    expected[1].DomainID,
					"test_string":  expected[1].TestString,
					"test_number":  expected[1].TestNumber,
					"test_integer": expected[1].TestInteger,
					"test_bool":    expected[1].TestBool,
				}),
				)
			})
		})
	})

	Describe("Generate Table", func() {
		var server *schema.Schema
		var subnet *schema.Schema
		var test *schema.Schema

		BeforeEach(func() {
			manager := schema.GetManager()
			var ok bool
			server, ok = manager.Schema("server")
			Expect(ok).To(BeTrue())
			subnet, ok = manager.Schema("subnet")
			Expect(ok).To(BeTrue())
			test, ok = manager.Schema("test")
			Expect(ok).To(BeTrue())
		})

		Context("Index on multiple columns", func() {
			It("Should create unique index on tenant_id, domain_id and id", func() {
				_, indices := sqlConn.GenTableDef(test, false)
				Expect(indices).To(HaveLen(3))
				Expect(indices[2]).To(ContainSubstring("CREATE UNIQUE INDEX test_unique_id_and_tenant_id_and_domain_id ON `tests`(`id`,`tenant_id`,`domain_id`);"))
			})
		})

		Context("Index in schema", func() {
			It("Should create index, if schema property should be indexed", func() {
				_, indices := sqlConn.GenTableDef(test, false)
				Expect(indices).To(HaveLen(3))
				Expect(indices[0]).To(ContainSubstring("CREATE INDEX tests_tenant_id_idx ON `tests`(`tenant_id`);"))
				Expect(indices[1]).To(ContainSubstring("CREATE INDEX tests_domain_id_idx ON `tests`(`domain_id`);"))
			})
		})

		Context("Relation column name", func() {
			It("Generate foreign key with default column name when relationColumn not available", func() {
				table, _ := sqlConn.GenTableDef(server, false)
				Expect(table).To(ContainSubstring("REFERENCES `networks`(id)"))
			})

			It("Generate foreign key with given column same as relationColumn from property", func() {
				prop := schema.NewPropertyBuilder("test", "test", "", "test").
					WithFormat("string").
					WithRelation("subnet", "cidr", "").
					WithSQLType("varchar(255)").
					Build()
				server.Properties = append(server.Properties, *prop)
				table, _, err := sqlConn.AlterTableDef(server, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(table).To(ContainSubstring("REFERENCES `subnets`(cidr)"))
			})
		})

		Context("With default cascade option", func() {
			It("Generate proper table with cascade delete", func() {
				table, _ := sqlConn.GenTableDef(server, true)
				Expect(table).To(ContainSubstring("REFERENCES `networks`(id) on delete cascade);"))
				table, _ = sqlConn.GenTableDef(subnet, true)
				Expect(table).To(ContainSubstring("REFERENCES `networks`(id) on delete cascade);"))
			})
		})

		Context("Without default cascade option", func() {
			It("Generate proper table with cascade delete", func() {
				table, _ := sqlConn.GenTableDef(server, false)
				Expect(table).To(ContainSubstring("REFERENCES `networks`(id) on delete cascade);"))
				table, _ = sqlConn.GenTableDef(subnet, false)
				Expect(table).ToNot(ContainSubstring("REFERENCES `networks`(id) on delete cascade);"))
			})
		})

		Context("Properties modifed", func() {
			It("Generate proper alter table statements", func() {
				prop := schema.NewPropertyBuilder("test", "test", "", "test").
					WithFormat("string").
					WithSQLType("varchar(255)").
					Build()
				server.Properties = append(server.Properties, *prop)
				table, _, err := sqlConn.AlterTableDef(server, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(table).To(ContainSubstring("alter table`servers` add (`test` varchar(255) not null);"))
			})

			It("Create index if property should be indexed", func() {
				prop := schema.NewPropertyBuilder("test", "test", "", "test").
					WithFormat("string").
					WithSQLType("varchar(255)").
					WithIndexed(true).
					Build()
				server.Properties = append(server.Properties, *prop)
				_, indices, err := sqlConn.AlterTableDef(server, true)
				Expect(err).ToNot(HaveOccurred())
				Expect(indices).To(HaveLen(1))
				Expect(indices[0]).To(ContainSubstring("CREATE INDEX servers_test_idx ON `servers`(`test`);"))
			})
		})
	})

	Describe("Query construction", func() {
		var (
			query         squirrel.SelectBuilder
			expectedQuery squirrel.SelectBuilder
		)

		BeforeEach(func() {
			t := testSchema.GetDbTableName()
			query = squirrel.Select("*").From(quote(t))
			expectedQuery = query
		})

		checkAndStatement := func(resSql, first, second string, params, expectedParams []interface{}) {
			// filter is map[string]interface{} thus order of the generated sql may differ (i.e. `first AND second` or `second AND first`)
			// we can only check each substring
			Expect(resSql).To(ContainSubstring(" AND "))
			Expect(resSql).To(ContainSubstring(first))
			Expect(resSql).To(ContainSubstring(second))
			Expect(params).To(ConsistOf(expectedParams))
		}

		checkLikeQueries := func(searchString string, queryString string) {
			search_value := search.NewSearchField(searchString)
			filter := map[string]interface{}{"test_string": search_value}

			query, err := AddFilterToSelectQuery(testSchema, query, filter, false)

			Expect(err).ToNot(HaveOccurred())
			resSql, param, err := query.ToSql()
			Expect(err).ToNot(HaveOccurred())
			expectedQuery = expectedQuery.Where(Like{"`test_string`": queryString})
			expectedSql, expectedParam, _ := expectedQuery.ToSql()
			Expect(resSql).To(Equal(expectedSql))
			Expect(param).To(Equal(expectedParam))
		}

		Context("Basic select query", func() {
			It("should create empty select query", func() {
				filter := map[string]interface{}{}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})
			It("should create select query with one parameter", func() {
				filter := map[string]interface{}{"test_string": "123"}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())
				expectedQuery = expectedQuery.Where(squirrel.Eq{"`test_string`": "123"})
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})
			It("should create LIKE query with one parameter", func() {
				checkLikeQueries("123", "%123%")
			})
			It("should create LIKE query with one parameter and escape special characters", func() {
				checkLikeQueries("\\1%2_3_a\\b%c", "%\\\\1\\%2\\_3\\_a\\\\b\\%c%")
			})
			It("should create LIKE query with two parameter", func() {
				string_search := search.NewSearchField("123")
				number_search := search.NewSearchField("42")
				filter := map[string]interface{}{"test_string": string_search, "test_number": number_search}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())
				checkAndStatement(resSql, "`test_string` LIKE ?", "`test_number` LIKE ?", param, []interface{}{"%123%", "%42%"})
			})
			It("should create LIKE query with one parameter and IN query with the other", func() {
				string_search := search.NewSearchField("one")
				filter := map[string]interface{}{"test_string": string_search, "test_number": 54}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())
				checkAndStatement(resSql, "`test_string` LIKE ?", "`test_number` = ?", param, []interface{}{"%one%", 54})
			})
			It("should create conjunction for more than one parameter by default", func() {
				filter := map[string]interface{}{"test_string": "123", "test_number": 42}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())
				checkAndStatement(resSql, "`test_string` = ?", "`test_number` = ?", param, []interface{}{42, "123"})
			})
		})
		checkLikeDisjunctionAndConjunction := func(searchString string, numberString string, filterType string, convertedString string, convertedNumber string) {
			string_search := search.NewSearchField(searchString)
			number_search := search.NewSearchField(numberString)
			filter := map[string]interface{}{filterType: []map[string]interface{}{
				{
					"property": "test_string",
					"value":    string_search,
				},
				{
					"property": "test_number",
					"value":    number_search,
				},
			}}

			res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

			Expect(err).ToNot(HaveOccurred())
			resSql, param, err := res.ToSql()
			Expect(err).ToNot(HaveOccurred())
			switch filterType {
			case "__or__":
				expectedQuery = expectedQuery.Where(squirrel.Or{Like{"`test_string`": convertedString}, Like{"`test_number`": convertedNumber}})
			case "__and__":
				expectedQuery = expectedQuery.Where(squirrel.And{Like{"`test_string`": convertedString}, Like{"`test_number`": convertedNumber}})
			}
			expectedSql, expectedParam, _ := expectedQuery.ToSql()
			Expect(resSql).To(Equal(expectedSql))
			Expect(param).To(Equal(expectedParam))
		}
		Context("Disjunction and conjunction", func() {
			It("should create disjunction for more then one parameter", func() {
				filter := map[string]interface{}{"__or__": []map[string]interface{}{
					{
						"property": "test_string",
						"type":     "eq",
						"value":    "123",
					},
					{
						"property": "test_number",
						"type":     "eq",
						"value":    42,
					},
				}}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())

				expectedQuery = expectedQuery.Where(squirrel.Or{squirrel.Eq{"`test_string`": "123"}, squirrel.Eq{"`test_number`": 42}})
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})
			It("should create disjunction for more than one parameter with like queries", func() {
				checkLikeDisjunctionAndConjunction("123", "42", "__or__", "%123%", "%42%")
			})
			It("should create conjunction for more then one parameter", func() {
				filter := map[string]interface{}{"__and__": []map[string]interface{}{
					{
						"property": "test_string",
						"type":     "eq",
						"value":    "123",
					},
					{
						"property": "test_number",
						"type":     "eq",
						"value":    42,
					},
				}}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())

				expectedQuery = expectedQuery.Where(squirrel.And{squirrel.Eq{"`test_string`": "123"}, squirrel.Eq{"`test_number`": 42}})
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})
			It("should create conjunction for more than one parameter with like queries", func() {
				checkLikeDisjunctionAndConjunction("123", "42", "__and__", "%123%", "%42%")
			})
			It("should create disjunction of conjunctions for more then one parameter", func() {
				filter := map[string]interface{}{
					"__or__": []map[string]interface{}{
						{
							"__and__": []map[string]interface{}{
								{
									"property": "test_string",
									"type":     "eq",
									"value":    "123",
								},
								{
									"property": "test_number",
									"type":     "eq",
									"value":    42,
								},
							},
						},
						{
							"property": "test_number",
							"type":     "eq",
							"value":    69,
						},
						{
							"property": "test_number",
							"type":     "eq",
							"value":    1024,
						},
						{
							"__or__": []map[string]interface{}{
								{
									"__and__": []map[string]interface{}{
										{
											"property": "test_string",
											"type":     "eq",
											"value":    "1024",
										},
										{
											"property": "test_number",
											"type":     "eq",
											"value":    123,
										},
									},
								},
								{
									"property": "test_string",
									"type":     "eq",
									"value":    "69",
								},
							},
						},
					},
				}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())

				expectedQuery = expectedQuery.Where(
					squirrel.Or{
						squirrel.And{squirrel.Eq{"`test_string`": "123"}, squirrel.Eq{"`test_number`": 42}},
						squirrel.Eq{"`test_number`": 69},
						squirrel.Eq{"`test_number`": 1024},
						squirrel.Or{
							squirrel.And{squirrel.Eq{"`test_string`": "1024"}, squirrel.Eq{"`test_number`": 123}},
							squirrel.Eq{"`test_string`": "69"},
						},
					})
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})
			It("should process both simple properties and disjunction", func() {
				filter := map[string]interface{}{
					"test_integer": 42,
					"__or__": []map[string]interface{}{
						{
							"property": "test_number",
							"type":     "eq",
							"value":    13,
						},
						{
							"property": "test_string",
							"type":     "neq",
							"value":    "123",
						},
					},
				}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())

				checkAndStatement(resSql, "`test_integer` = ?", "(`test_number` = ? OR `test_string` <> ?", param, []interface{}{13, 42, "123"})
			})
			It("should process various operators", func() {
				filter := map[string]interface{}{
					"__or__": []map[string]interface{}{
						{
							"property": "test_string",
							"type":     "eq",
							"value":    "123",
						},
						{
							"property": "test_string",
							"value":    search.NewSearchField("123"),
						},
						{
							"property": "test_number",
							"type":     "eq",
							"value":    []int{0, 1, 2},
						},
						{
							"property": "test_bool",
							"type":     "neq",
							"value":    true,
						},
						{
							"property": "test_integer",
							"type":     "neq",
							"value":    []int{0, 1, 2},
						},
					},
				}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())

				expectedQuery = expectedQuery.Where(
					squirrel.Or{
						squirrel.Eq{"`test_string`": "123"},
						Like{"`test_string`": "%123%"},
						squirrel.Eq{"`test_number`": []int{0, 1, 2}},
						squirrel.NotEq{"`test_bool`": true},
						squirrel.NotEq{"`test_integer`": []int{0, 1, 2}},
					})
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})
			It("should process one property in disjunction statement", func() {
				filter := map[string]interface{}{
					"__or__": []map[string]interface{}{
						{
							"property": "test_string",
							"type":     "neq",
							"value":    "123",
						},
					},
				}

				res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

				Expect(err).ToNot(HaveOccurred())
				resSql, param, err := res.ToSql()
				Expect(err).ToNot(HaveOccurred())

				expectedQuery = expectedQuery.Where(squirrel.Or{squirrel.NotEq{"`test_string`": "123"}})
				expectedSql, expectedParam, _ := expectedQuery.ToSql()
				Expect(resSql).To(Equal(expectedSql))
				Expect(param).To(Equal(expectedParam))
			})

			DescribeTable("should handle empty list in WHERE ... IN ()",
				func(filter map[string]interface{}, expected string) {
					res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

					Expect(err).ToNot(HaveOccurred())
					resSql, param, err := res.ToSql()
					Expect(err).ToNot(HaveOccurred())

					expectedQuery = expectedQuery.Where(expected)
					expectedSql, expectedParam, _ := expectedQuery.ToSql()
					Expect(resSql).To(Equal(expectedSql))
					Expect(param).To(Equal(expectedParam))
				},
				Entry("simple query translates to False",
					map[string]interface{}{"test_string": []string{}},
					"(1=0)"),
				Entry("eq translates to False",
					map[string]interface{}{
						"__or__": []map[string]interface{}{
							{
								"property": "test_number",
								"type":     "eq",
								"value":    []int{},
							},
						},
					},
					"((1=0))"),
				Entry("neq translates to True",
					map[string]interface{}{
						"__or__": []map[string]interface{}{
							{
								"property": "test_string",
								"type":     "neq",
								"value":    []string{},
							},
						},
					},
					"((1=1))",
				),
			)

			DescribeTable("should handle constant bool conditions",
				func(filter map[string]interface{}, expected interface{}) {
					res, err := AddFilterToSelectQuery(testSchema, query, filter, false)

					Expect(err).ToNot(HaveOccurred())
					resSql, param, err := res.ToSql()
					Expect(err).ToNot(HaveOccurred())

					expectedQuery = expectedQuery.Where(expected)
					expectedSql, expectedParam, _ := expectedQuery.ToSql()
					Expect(resSql).To(Equal(expectedSql))
					Expect(param).To(Equal(expectedParam))
				},
				Entry("always true, in condition root",
					map[string]interface{}{"__bool__": true},
					"(1=1)",
				),
				Entry("always false, in condition root",
					map[string]interface{}{"__bool__": false},
					"(1=0)",
				),
				Entry("always true, in compound condition",
					map[string]interface{}{
						"__or__": []map[string]interface{}{
							{
								"__bool__": true,
							},
							{
								"property": "test_string",
								"type":     "neq",
								"value":    "123",
							},
						},
					},
					squirrel.Or{squirrel.Expr("(1=1)"), squirrel.NotEq{"`test_string`": "123"}},
				),
				Entry("always false, in compound condition",
					map[string]interface{}{
						"__or__": []map[string]interface{}{
							{
								"__bool__": false,
							},
							{
								"property": "test_string",
								"type":     "neq",
								"value":    "123",
							},
						},
					},
					squirrel.Or{squirrel.Expr("(1=0)"), squirrel.NotEq{"`test_string`": "123"}},
				),
			)
		})
	})
})

func quote(str string) string {
	return fmt.Sprintf("`%s`", str)
}

func readFixtures(path string, v interface{}) {
	f, err := os.Open(path)
	if err != nil {
		panic("failed to open test fixtures")
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(&v)
	if err != nil {
		panic("failed parse test fixtures")
	}
}
