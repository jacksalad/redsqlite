import { describe, expect, it } from "vitest";
import { formatBytes } from "./format";

describe("formatBytes", () => {
  it("字节级", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1023)).toBe("1023 B");
  });

  it("KB / MB", () => {
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(5 * 1024)).toBe("5.0 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5.00 MB");
  });
});
