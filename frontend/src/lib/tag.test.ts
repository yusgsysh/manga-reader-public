import { describe, expect, it } from "vitest";
import { parseTagString } from "../lib/tag";

describe("parseTagString", () => {
  it("parses namespace:name", () => {
    expect(parseTagString("female:yuri")).toEqual({
      namespace: "female",
      name: "yuri",
    });
  });

  it("parses name without namespace", () => {
    expect(parseTagString("full color")).toEqual({
      namespace: "",
      name: "full color",
    });
  });

  it("keeps colons inside the name", () => {
    expect(parseTagString("artist:foo:bar")).toEqual({
      namespace: "artist",
      name: "foo:bar",
    });
  });
});
