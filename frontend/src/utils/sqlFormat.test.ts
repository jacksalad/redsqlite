import { describe, expect, it } from "vitest";
import { formatSql } from "./sqlFormat";

describe("formatSql", () => {
  it("空输入返回空串", () => {
    expect(formatSql("")).toBe("");
    expect(formatSql("   ")).toBe("");
  });

  it("关键字大写、主要子句换行、运算符两侧留空格", () => {
    const out = formatSql(
      "select id,name from users where age>18 and name='tom' order by id desc limit 10",
    );
    expect(out).toContain("SELECT");
    expect(out).toContain("\nFROM users");
    expect(out).toContain("\nWHERE");
    expect(out).toContain("age > 18");
    expect(out).toContain("name = 'tom'");
    expect(out).toContain("\nORDER");
    expect(out).toContain("\nLIMIT 10");
  });

  it("字符串字面量原样保留（不大写、不拆分）", () => {
    const out = formatSql("select 'SELECT from where' as label");
    expect(out).toContain("'SELECT from where'");
  });

  it("注释独占一行且原样保留", () => {
    const out = formatSql("select 1 -- trailing comment");
    expect(out).toContain("-- trailing comment");
  });

  it("两字符运算符合并", () => {
    expect(formatSql("select * from t where a>=10")).toContain("a >= 10");
    expect(formatSql("select * from t where a<>b")).toContain("a <> b");
  });

  it("LEFT JOIN 整体一行换行", () => {
    const out = formatSql("select * from a left join b on a.id=b.id");
    expect(out).toContain("\nLEFT JOIN b");
    expect(out).toContain("a.id = b.id");
  });
});
