import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { project as leitner } from "../core/leitner.js";
import { attentionWindowSeconds } from "../core/attention.js";
import { project as tasks } from "../core/tasks.js";
import { project as decks } from "../core/decks.js";
import { project as stats } from "../core/stats.js";
import { project as inbox } from "../core/inbox.js";
import { project as calendar, reminders } from "../core/calendar.js";
import { project as review } from "../core/review.js";

const vectorsDir = join(
  dirname(fileURLToPath(import.meta.url)),
  "..", "..", "spec", "vectors"
);

const PERMUTATION_RUNS = 5;

function runProjection(vector, events) {
  switch (vector.projection) {
    case "leitner":
      return leitner(events, { now: vector.now, tz: vector.tz });
    case "attention":
      return { attention_seconds: attentionWindowSeconds(events) };
    case "tasks":
      return tasks(events);
    case "decks":
      return decks(events);
    case "stats":
      return stats(events, { now: vector.now, tz: vector.tz });
    case "inbox":
      return inbox(events);
    case "calendar":
      return calendar(events, { now: vector.now, tz: vector.tz });
    case "reminders":
      return reminders(events, { now: vector.now, tz: vector.tz });
    case "review":
      return review(events, { now: vector.now, tz: vector.tz });
    default:
      throw new Error(`projeção desconhecida: ${vector.projection}`);
  }
}

// Asserção por inclusão parcial (ver spec/SPEC.md §6): todo campo presente em
// expected deve bater; arrays são comparados por igualdade exata.
function expectMatch(actual, expected, path = "$") {
  if (Array.isArray(expected)) {
    expect(actual, path).toEqual(expected);
    return;
  }
  if (expected !== null && typeof expected === "object") {
    expect(actual, path).toBeTypeOf("object");
    for (const key of Object.keys(expected)) {
      expectMatch(actual?.[key], expected[key], `${path}.${key}`);
    }
    return;
  }
  expect(actual, path).toBe(expected);
}

// Embaralhamento determinístico (mulberry32) para os vetores de convergência.
function shuffled(array, seed) {
  let s = seed >>> 0;
  const rand = () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const copy = [...array];
  for (let i = copy.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [copy[i], copy[j]] = [copy[j], copy[i]];
  }
  return copy;
}

const files = readdirSync(vectorsDir).filter((f) => f.endsWith(".json")).sort();

describe("vetores de conformidade (spec/vectors)", () => {
  for (const file of files) {
    const vector = JSON.parse(readFileSync(join(vectorsDir, file), "utf8"));

    describe(`${file}: ${vector.name}`, () => {
      it("produz o estado esperado", () => {
        expectMatch(runProjection(vector, vector.events), vector.expected);
      });

      if (vector.permutations) {
        for (let seed = 1; seed <= PERMUTATION_RUNS; seed++) {
          it(`converge com a entrada embaralhada (permutação ${seed})`, () => {
            const events = shuffled(vector.events, seed);
            expectMatch(runProjection(vector, events), vector.expected);
          });
        }
      }
    });
  }
});
