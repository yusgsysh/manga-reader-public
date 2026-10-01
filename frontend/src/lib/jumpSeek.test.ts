import { describe, expect, it } from "vitest";
import { parseJumpSeekInput } from "./jumpSeek";

describe("parseJumpSeekInput", () => {
  it("classifies a bare year as seek", () => {
    expect(parseJumpSeekInput("2020")).toEqual({ kind: "seek", value: "2020" });
  });

  it("classifies dashed dates as seek", () => {
    expect(parseJumpSeekInput("2020-01")).toEqual({
      kind: "seek",
      value: "2020-01",
    });
    expect(parseJumpSeekInput("20-01-15")).toEqual({
      kind: "seek",
      value: "20-01-15",
    });
  });

  it("classifies relative offsets as jump", () => {
    expect(parseJumpSeekInput("1w")).toEqual({ kind: "jump", value: "1w" });
    expect(parseJumpSeekInput("3d")).toEqual({ kind: "jump", value: "3d" });
    expect(parseJumpSeekInput("30")).toEqual({ kind: "jump", value: "30" });
  });

  it("treats an out-of-range year as jump", () => {
    expect(parseJumpSeekInput("1999")).toEqual({ kind: "jump", value: "1999" });
  });

  it("trims surrounding whitespace", () => {
    expect(parseJumpSeekInput("  2020  ")).toEqual({
      kind: "seek",
      value: "2020",
    });
  });

  it("returns null for empty or invalid input", () => {
    expect(parseJumpSeekInput("")).toBeNull();
    expect(parseJumpSeekInput("   ")).toBeNull();
    expect(parseJumpSeekInput("abc")).toBeNull();
    expect(parseJumpSeekInput("1x")).toBeNull();
  });
});
