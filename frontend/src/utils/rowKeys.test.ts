import { describe, expect, it } from "vitest";
import type { CellValue } from "../types";
import { keyMapOfRow, keyOfRow } from "./rowKeys";

const cols = ["id", "name", "note"];

describe("keyMapOfRow", () => {
  it("按主键列提取定位映射", () => {
    expect(keyMapOfRow([1, "a", "x"], cols, ["id"])).toEqual({ id: 1 });
  });

  it("复合主键提取多列", () => {
    expect(keyMapOfRow([1, 2, "x"], cols, ["id", "name"])).toEqual({ id: 1, name: 2 });
  });

  it("无主键时回退首列（keyColumns 为 null 或空数组）", () => {
    expect(keyMapOfRow([7, "a", "x"], cols, null)).toEqual({ id: 7 });
    expect(keyMapOfRow([7, "a", "x"], cols, [])).toEqual({ id: 7 });
  });

  it("BLOB / bigint 标记原样保留（后端负责还原）", () => {
    const row: CellValue[] = [
      { __kind: "blob", size: 3, b64: "QUJD" },
      { __kind: "bigint", str: "9007199254740993" },
      "x",
    ];
    expect(keyMapOfRow(row, cols, ["id", "name"])).toEqual({
      id: { __kind: "blob", size: 3, b64: "QUJD" },
      name: { __kind: "bigint", str: "9007199254740993" },
    });
  });

  it("截断文本标记不可作定位值，置 null", () => {
    const row: CellValue[] = [
      { __kind: "text-truncated", size: 100, truncated: true, value: "ab" },
    ];
    expect(keyMapOfRow(row, ["id"], ["id"])).toEqual({ id: null });
  });
});

describe("keyOfRow", () => {
  it("是定位映射的 JSON 序列化", () => {
    expect(keyOfRow([1, "a", "x"], cols, ["id"])).toBe(JSON.stringify({ id: 1 }));
  });
});
