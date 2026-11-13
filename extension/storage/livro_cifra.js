// O EPUB no bucket, cifrado (spec/SPEC.md §5, "Livros no bucket") — o mesmo
// formato de cli/internal/sync/livro.go, travado pelo vetor cruzado
// spec/sync/livro_cifrado.json:
//
//   "PNNLIVRO" | versão 0x01 | iterações (uint32 BE) | sal (16) | nonce (12) |
//   AES-256-GCM(EPUB)  (texto cifrado + tag de 16)
//
// A chave sai da senha dos livros por PBKDF2-HMAC-SHA256; o AAD é o
// cabeçalho inteiro mais o book_id. Tudo em Web Crypto, sem dependência. O
// formato é público (o repositório é público): quem protege o livro é a
// senha, não o formato.

const MAGIC = new TextEncoder().encode("PNNLIVRO");
const VERSION = 1;
const SALT = 16;
const NONCE = 12;
const HEADER = MAGIC.length + 1 + 4 + SALT + NONCE;

export const BOOK_ITERATIONS = 600_000;
const MAX_ITERATIONS = 10_000_000;
export const BOOK_SUFFIX = ".pnnlivro";

export const bookKey = (prefix, bookId) => `${prefix}livros/${bookId}${BOOK_SUFFIX}`;

export class BookAuthError extends Error {
  constructor() {
    super("não abriu: a senha dos livros não confere (ou o arquivo foi alterado)");
    this.name = "BookAuthError";
  }
}

async function deriveKey(passphrase, salt, iterations) {
  const base = await crypto.subtle.importKey("raw", new TextEncoder().encode(passphrase), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", hash: "SHA-256", salt, iterations },
    base,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

function aad(header, bookId) {
  const id = new TextEncoder().encode(bookId);
  const out = new Uint8Array(header.length + id.length);
  out.set(header);
  out.set(id, header.length);
  return out;
}

/**
 * Cifra o EPUB. `salt`/`nonce` só nos testes (o vetor cruzado); em uso,
 * aleatórios.
 */
export async function sealBook(passphrase, bookId, epub, { iterations = BOOK_ITERATIONS, salt, nonce } = {}) {
  if (!passphrase) throw new Error("senha dos livros vazia");
  salt ??= crypto.getRandomValues(new Uint8Array(SALT));
  nonce ??= crypto.getRandomValues(new Uint8Array(NONCE));
  const header = new Uint8Array(HEADER);
  header.set(MAGIC);
  header[MAGIC.length] = VERSION;
  new DataView(header.buffer).setUint32(MAGIC.length + 1, iterations);
  header.set(salt, MAGIC.length + 5);
  header.set(nonce, MAGIC.length + 5 + SALT);

  const key = await deriveKey(passphrase, salt, iterations);
  const sealed = new Uint8Array(await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: nonce, additionalData: aad(header, bookId) },
    key,
    epub,
  ));
  const blob = new Uint8Array(HEADER + sealed.length);
  blob.set(header);
  blob.set(sealed, HEADER);
  return blob;
}

export async function openBook(passphrase, bookId, blob) {
  const isBook = blob.length >= HEADER + 16 && MAGIC.every((b, i) => blob[i] === b) && blob[MAGIC.length] === VERSION;
  if (!isBook) throw new Error("não é um livro do pnn (.pnnlivro)");
  const iterations = new DataView(blob.buffer, blob.byteOffset).getUint32(MAGIC.length + 1);
  if (iterations < 1 || iterations > MAX_ITERATIONS) throw new Error("não é um livro do pnn (.pnnlivro)");
  const header = blob.slice(0, HEADER);
  const salt = header.slice(MAGIC.length + 5, MAGIC.length + 5 + SALT);
  const nonce = header.slice(MAGIC.length + 5 + SALT);
  const key = await deriveKey(passphrase, salt, iterations);
  try {
    return new Uint8Array(await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: nonce, additionalData: aad(header, bookId) },
      key,
      blob.slice(HEADER),
    ));
  } catch {
    throw new BookAuthError();
  }
}
