import { describe, expect, it } from "vitest";
import { narrowRows } from "./rows";

describe("narrowRows", () => {
  it("过滤 null 行元素", () => {
    expect(narrowRows([[1, "a"], null, [2, "b"]])).toEqual([
      [1, "a"],
      [2, "b"],
    ]);
  });

  it("外层 null / undefined / 空数组都收窄为空数组", () => {
    expect(narrowRows(null)).toEqual([]);
    expect(narrowRows(undefined)).toEqual([]);
    expect(narrowRows([])).toEqual([]);
  });

  it("保留行内的 null 单元格值", () => {
    expect(narrowRows([[1, null], [null, "x"]])).toEqual([[1, null], [null, "x"]]);
  });
});
