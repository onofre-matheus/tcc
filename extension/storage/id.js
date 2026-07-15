// Geração de identificadores UUIDv7 (spec/SPEC.md §1).
// Prefixo de timestamp de 48 bits + versão/variante + aleatório — os ids
// crescem monotonicamente no tempo, o que ajuda ordenação e depuração.
// `crypto.getRandomValues` existe tanto no navegador quanto no Node 22.

export function uuidv7(ms = Date.now()) {
  const bytes = new Uint8Array(16);

  let t = ms;
  for (let i = 5; i >= 0; i--) {
    bytes[i] = t % 256;
    t = Math.floor(t / 256);
  }

  crypto.getRandomValues(bytes.subarray(6));
  bytes[6] = (bytes[6] & 0x0f) | 0x70; // versão 7
  bytes[8] = (bytes[8] & 0x3f) | 0x80; // variante RFC 4122

  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0"));
  return (
    hex.slice(0, 4).join("") +
    "-" +
    hex.slice(4, 6).join("") +
    "-" +
    hex.slice(6, 8).join("") +
    "-" +
    hex.slice(8, 10).join("") +
    "-" +
    hex.slice(10, 16).join("")
  );
}
