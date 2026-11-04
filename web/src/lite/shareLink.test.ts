import { describe, expect, it } from "vitest";
import { buildShareLink, takeSecretFromHash } from "./shareLink";

const token = "fixture_token-0123456789abcdefABCDEF0123456789";

describe("share link", () => {
  it("reads a token out of the fragment", () => {
    expect(takeSecretFromHash("#" + token)).toBe(token);
    expect(takeSecretFromHash(token)).toBe(token);
  });
  it("ignores short, empty or odd fragments", () => {
    expect(takeSecretFromHash("")).toBeNull();
    expect(takeSecretFromHash("#")).toBeNull();
    expect(takeSecretFromHash("#short")).toBeNull();
    expect(takeSecretFromHash("#" + token + "/path?x=1")).toBeNull();
  });
  it("builds the link without a double slash", () => {
    expect(buildShareLink("https://lite.example.app/", token)).toBe(`https://lite.example.app/#${token}`);
    expect(buildShareLink("https://lite.example.app", token)).toBe(`https://lite.example.app/#${token}`);
  });
});
