// O livro cifrado da extensão é o mesmo da CLI, byte a byte: o vetor
// spec/sync/livro_cifrado.json é lido aqui e em cli/internal/sync — o EPUB
// cadastrado no navegador abre no terminal, e vice-versa.

import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { sealBook, openBook, BookAuthError, bookKey, BOOK_ITERATIONS } from "../storage/livro_cifra.js";

const vector = JSON.parse(readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "..", "..", "spec", "sync", "livro_cifrado.json"), "utf8"));
const fromHex = (h) => Uint8Array.from(h.match(/../g), (x) => parseInt(x, 16));
const fromB64 = (b) => Uint8Array.from(Buffer.from(b, "base64"));

describe("livro cifrado (.pnnlivro): vetor cruzado com a CLI", () => {
  const epub = fromB64(vector.epub_base64);
  const blob = fromB64(vector.blob_base64);

  it("selar com sal e nonce fixos dá o mesmo blob do Go", async () => {
    const sealed = await sealBook(vector.passphrase, vector.book_id, epub, {
      iterations: vector.iterations, salt: fromHex(vector.salt_hex), nonce: fromHex(vector.nonce_hex),
    });
    expect(Buffer.from(sealed).toString("base64")).toBe(vector.blob_base64);
  });

  it("abrir o blob do Go devolve o EPUB", async () => {
    expect(await openBook(vector.passphrase, vector.book_id, blob)).toEqual(epub);
  });

  for (const f of vector.must_fail) {
    it(`falha: ${f.why}`, async () => {
      const b = blob.slice();
      if (f.flip_byte !== undefined) b[f.flip_byte] ^= 0xff;
      await expect(openBook(f.passphrase ?? vector.passphrase, f.book_id ?? vector.book_id, b))
        .rejects.toBeInstanceOf(BookAuthError);
    });
  }

  it("em uso: 600 000 iterações, sal e nonce aleatórios, conteúdo invisível", async () => {
    const a = await sealBook("s", "b1", new TextEncoder().encode("epub legível"));
    const b = await sealBook("s", "b1", new TextEncoder().encode("epub legível"));
    expect(new DataView(a.buffer).getUint32(9)).toBe(BOOK_ITERATIONS);
    expect(Buffer.from(a).equals(Buffer.from(b))).toBe(false);
    expect(Buffer.from(a).includes("legível")).toBe(false);
    expect(bookKey("pnn/", "b1")).toBe("pnn/livros/b1.pnnlivro");
    await expect(openBook("s", "b1", new TextEncoder().encode("PK não é um pnnlivro, nem de longe, mesmo"))).rejects.toThrow("não é um livro do pnn");
  });
});
