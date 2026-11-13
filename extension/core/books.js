// Livros e anotações (catálogo 1.7, spec/SPEC.md §3 "Livros" e §4.16): o
// livro cadastrado é a identidade (título, autor, formato, sha256 do
// arquivo) — o arquivo em si e o caminho dele ficam no dispositivo, fora do
// log. A anotação aponta para uma posição no livro (capítulo do spine +
// parágrafo, e a página impressa quando o EPUB a traz) e guarda o trecho
// citado, que reancora a anotação se a extração mudar. Edição é LWW por
// campo; apagar anotação é tombstone; o "onde parei" é o último na ordem
// total. Abrir o EPUB é da CLI; aqui é só a projeção, com paridade testada
// pelos mesmos vetores.

import { normalize } from "./envelope.js";

const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

// Os dois núcleos limitam os índices ao mesmo inteiro.
const MAX_LOC_INDEX = 1_000_000_000;

const positiveInt = (v) => Number.isInteger(v) && v >= 1 && v <= MAX_LOC_INDEX;

const trimmed = (v) => (typeof v === "string" ? v.trim() : "");

// parseLoc: chapter e para inteiros ≥ 1; page só se for texto não vazio.
// Inválida = null (o evento é ignorado).
export function parseLoc(raw) {
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return null;
  if (!positiveInt(raw.chapter) || !positiveInt(raw.para)) return null;
  const loc = { chapter: raw.chapter, para: raw.para };
  const page = trimmed(raw.page);
  if (page) loc.page = page;
  return loc;
}

export function project(events) {
  const books = {};
  const notes = {};
  const deleted = new Set();

  for (const event of normalize(events)) {
    const p = event.payload ?? {};
    switch (event.type) {
      case "book.registered": {
        const title = trimmed(p.title);
        if (!p.book_id || !title || books[p.book_id]) break;
        books[p.book_id] = {
          title,
          author: trimmed(p.author),
          format: trimmed(p.format),
          sha256: trimmed(p.sha256),
          archived: false,
          progress: null,
          progress_percent: null,
          progress_at: null,
          created_at: event.ts,
          updated_at: event.ts,
          annotations: [],
        };
        break;
      }
      case "book.edited": {
        const book = books[p.book_id];
        if (!book) break;
        let edited = false;
        if (typeof p.title === "string" && p.title.trim()) {
          book.title = p.title.trim();
          edited = true;
        }
        if (typeof p.author === "string") {
          book.author = p.author.trim();
          edited = true;
        }
        if (edited) book.updated_at = event.ts;
        break;
      }
      case "book.archived":
        if (books[p.book_id]) books[p.book_id].archived = true;
        break;
      case "book.progressed": {
        const book = books[p.book_id];
        const loc = parseLoc(p.loc);
        if (!book || !loc) break;
        book.progress = loc;
        book.progress_percent = Number.isInteger(p.percent) && p.percent >= 0 && p.percent <= 100 ? p.percent : null;
        book.progress_at = event.ts;
        book.updated_at = event.ts;
        break;
      }
      case "annotation.created": {
        const book = books[p.book_id];
        const loc = parseLoc(p.loc);
        const quote = trimmed(p.quote);
        const id = p.annotation_id;
        if (!id || notes[id] || deleted.has(id) || !book || !loc || !quote) break;
        notes[id] = {
          book_id: p.book_id,
          loc,
          quote,
          text: trimmed(p.text),
          created_at: event.ts,
          updated_at: event.ts,
        };
        book.updated_at = event.ts;
        break;
      }
      case "annotation.edited": {
        const note = notes[p.annotation_id];
        if (!note || typeof p.text !== "string") break;
        note.text = p.text.trim();
        note.updated_at = event.ts;
        books[note.book_id].updated_at = event.ts;
        break;
      }
      case "annotation.deleted": {
        const note = notes[p.annotation_id];
        if (!note) break;
        delete notes[p.annotation_id];
        deleted.add(p.annotation_id);
        books[note.book_id].updated_at = event.ts;
        break;
      }
    }
  }

  for (const [id, note] of Object.entries(notes)) books[note.book_id].annotations.push(id);
  for (const book of Object.values(books)) {
    book.annotations.sort((a, b) => {
      const na = notes[a], nb = notes[b];
      return (na.loc.chapter - nb.loc.chapter)
        || (na.loc.para - nb.loc.para)
        || cmp(na.created_at, nb.created_at)
        || cmp(a, b);
    });
  }

  const order = Object.keys(books)
    .filter((id) => !books[id].archived)
    .sort((a, b) => cmp(books[b].updated_at, books[a].updated_at) || cmp(a, b));
  return { books, annotations: notes, order };
}
