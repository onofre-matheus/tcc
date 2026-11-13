// Cadastro de livro pela extensão (catálogo 1.7; SPEC §5, "Livros no
// bucket"): o EPUB sobe cifrado ANTES do book.registered — envio que falha
// não deixa cadastro sem arquivo —, o mesmo arquivo não cadastra duas vezes,
// e sem a senha dos livros nada acontece.

import { describe, expect, it } from "vitest";
import { epubZip } from "./zip-helper.js";

const storage = new Map();
globalThis.chrome = {
  storage: {
    local: {
      async get(keys) {
        const wanted = typeof keys === "string" ? [keys] : Array.isArray(keys) ? keys : [];
        const out = {};
        for (const k of wanted) if (storage.has(k)) out[k] = storage.get(k);
        return out;
      },
      async set(items) {
        for (const [k, v] of Object.entries(items)) storage.set(k, v);
      },
      async remove(key) {
        storage.delete(key);
      },
    },
    managed: undefined,
    onChanged: { addListener() {} },
  },
};

const store = await import("../ui/store.js");
const { openBook } = await import("../storage/livro_cifra.js");

function fakeBucket({ failPut = false } = {}) {
  const objects = new Map();
  return {
    objects,
    async list(prefix) {
      return [...objects.keys()].filter((k) => k.startsWith(prefix)).map((key) => ({ key, etag: "x" }));
    },
    async put(key, body) {
      if (failPut) throw new Error("S3 PUT 403: Access Denied");
      objects.set(key, body);
      return "etag";
    },
  };
}

const OPF = `<package><metadata><dc:title>Mastering Your Adult ADHD</dc:title><dc:creator>Safren</dc:creator></metadata></package>`;
const config = { prefix: "pnn/", bookPassphrase: "s3nha" };
const registered = async () => (await store.log.events()).filter((e) => e.type === "book.registered");

describe("registerBook", () => {
  const epub = epubZip(OPF);

  it("sem a senha dos livros, recusa e não grava nada", async () => {
    const bucket = fakeBucket();
    await expect(store.registerBook(epub, "a.epub", { config: { prefix: "pnn/" }, bucket })).rejects.toBeInstanceOf(store.BookPassphraseMissing);
    expect(bucket.objects.size).toBe(0);
    expect(await registered()).toHaveLength(0);
  });

  it("envio que falha não cadastra", async () => {
    await expect(store.registerBook(epub, "a.epub", { config, bucket: fakeBucket({ failPut: true }) })).rejects.toThrow("403");
    expect(await registered()).toHaveLength(0);
  });

  it("cifra, envia e só então cadastra; a CLI abre com a mesma senha", async () => {
    const bucket = fakeBucket();
    const r = await store.registerBook(epub, "a.epub", { config, bucket });
    expect(r).toMatchObject({ title: "Mastering Your Adult ADHD", created: true, uploaded: true });
    const [key] = [...bucket.objects.keys()];
    expect(key).toBe(`pnn/livros/${r.bookId}.pnnlivro`);
    expect(await openBook("s3nha", r.bookId, bucket.objects.get(key))).toEqual(epub);
    const [event] = await registered();
    const sha = [...new Uint8Array(await crypto.subtle.digest("SHA-256", epub))].map((b) => b.toString(16).padStart(2, "0")).join("");
    expect(event.payload).toEqual({ book_id: r.bookId, title: "Mastering Your Adult ADHD", author: "Safren", format: "epub", sha256: sha });

    // o mesmo arquivo: não cadastra de novo; com o objeto no bucket, nem envia
    const again = await store.registerBook(epub, "copia.epub", { config, bucket });
    expect(again).toMatchObject({ bookId: r.bookId, created: false, uploaded: false });
    expect(await registered()).toHaveLength(1);

    // noutro bucket (sem o objeto), reenvia
    const other = fakeBucket();
    const resent = await store.registerBook(epub, "a.epub", { config, bucket: other });
    expect(resent).toMatchObject({ created: false, uploaded: true });
    expect(other.objects.size).toBe(1);
  });
});
