// Normalização e entrada de tags (spec/SPEC.md §3) — mesma tabela do cliente
// Go (cli/core/tags_test.go).
import { describe, it, expect } from "vitest";
import { normalizeTag, parseTags, renameTag } from "../core/tags.js";
import { parseNode, nodeKey } from "../core/links.js";

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

describe("renameTag", () => {
  const cases = [
    ["so", "so", "sistemas", "sistemas"],
    ["so/threads/posix", "so", "sistemas", "sistemas/threads/posix"],
    ["sobre", "so", "sistemas", "sobre"], // prefixo de texto não é descendente
    ["redes", "so", "sistemas", "redes"],
    ["so/threads", "so/threads", "so/concorrencia", "so/concorrencia"],
    ["so/x", "so", "so/base", "so/base/x"], // renomear para baixo da própria tag
  ];
  for (const [tag, from, to, want] of cases) {
    it(`${tag} (${from} → ${to}) → ${want}`, () => {
      expect(renameTag(tag, from, to)).toBe(want);
    });
  }
});

describe("parseNode", () => {
  const cases = {
    "card:c1": "card:c1",
    "tag:#SO/Threads ": "tag:so/threads",
    "deck: d1 ": "deck:d1",
    "tag:a:b": "tag:a:b", // o id pode ter ":" — corta no primeiro
    "note:n1": "", // kind desconhecido
    "tag: # ": "", // tag vazia
    "c1": "", // sem kind
  };
  for (const [key, want] of Object.entries(cases)) {
    it(`${JSON.stringify(key)} → ${JSON.stringify(want)}`, () => {
      const node = parseNode(key);
      expect(node ? nodeKey(node) : "").toBe(want);
    });
  }
});
