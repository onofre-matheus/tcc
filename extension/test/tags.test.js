// Normalização e entrada de tags (spec/SPEC.md §3) — mesma tabela do cliente
// Go (cli/core/tags_test.go).
import { describe, it, expect } from "vitest";
import { normalizeTag, parseTags } from "../core/tags.js";

describe("normalizeTag", () => {
  const cases = {
    "so": "so",
    "#SO": "so",
    "  ##Prova  P2 ": "prova-p2",
    "/SO// Threads /": "so/threads",
    "# Memória Virtual": "memória-virtual",
    "so / escalonamento": "so/escalonamento",
    "#": "",
    "  ": "",
    "//": "",
  };
  for (const [raw, want] of Object.entries(cases)) {
    it(`${JSON.stringify(raw)} → ${JSON.stringify(want)}`, () => {
      expect(normalizeTag(raw)).toBe(want);
    });
  }
});

describe("parseTags", () => {
  const cases = {
    "#SO #Prova-P2, redes": ["so", "prova-p2", "redes"],
    "so,so  #so": ["so"],
    "so/escalonamento,,  ,": ["so/escalonamento"],
    "": [],
    "  # , ": [],
  };
  for (const [input, want] of Object.entries(cases)) {
    it(`${JSON.stringify(input)} → ${JSON.stringify(want)}`, () => {
      expect(parseTags(input)).toEqual(want);
    });
  }
});
