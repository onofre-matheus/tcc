// O essencial de um EPUB, no navegador: é EPUB? tem DRM? título e autor?
// (catálogo 1.7, cadastro de livro pela extensão). Ler o livro é da CLI; aqui
// só se confere o arquivo e se tira o que o book.registered leva — as mesmas
// recusas e mensagens da CLI (cli/internal/cli/livros.go, internal/epub).
//
// Sem dependência: o EPUB é um ZIP — o diretório central se lê à mão e o
// deflate sai pelo DecompressionStream do navegador. O XML do pacote é
// estreito e conhecido, então um extrator por expressão regular resolve,
// como o ListObjectsV2 em storage/s3.js (e funciona sem DOM).

const EOCD = 0x06054b50;
const CENTRAL = 0x02014b50;
const LOCAL = 0x04034b50;

// Algoritmos que só ofuscam fontes embutidas — não são DRM do texto.
const FONT_OBFUSCATION = new Set(["http://www.idpf.org/2008/embedding", "http://ns.adobe.com/pdf/enc#RC"]);

const notEpub = (why) => new Error(`não é um EPUB válido: ${why}`);

// checkBookName recusa cedo, pelo nome, o que o pnn não lê (as mesmas
// mensagens da CLI).
export function checkBookName(name) {
  const ext = String(name).toLowerCase().match(/\.[^.]+$/)?.[0] ?? "";
  if ([".mobi", ".azw", ".azw3", ".kfx", ".prc"].includes(ext)) {
    const base = String(name).replace(/\.[^.]+$/, "");
    return `MOBI/AZW é formato fechado da Amazon e o pnn não lê — converta para EPUB com o Calibre: ebook-convert "${name}" "${base}.epub"`;
  }
  if (ext === ".pdf") {
    return "o pnn lê EPUB, não PDF: o texto de um PDF vem quebrado no terminal (cabeçalhos, hifenização, colunas) — se o livro existir em EPUB, use essa versão";
  }
  return null;
}

function readZip(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let eocd = -1;
  for (let i = bytes.length - 22; i >= Math.max(0, bytes.length - 22 - 0xffff); i--) {
    if (view.getUint32(i, true) === EOCD) {
      eocd = i;
      break;
    }
  }
  if (eocd < 0) throw notEpub("não é um arquivo ZIP");
  const count = view.getUint16(eocd + 10, true);
  let at = view.getUint32(eocd + 16, true);
  const entries = new Map();
  const decoder = new TextDecoder();
  for (let n = 0; n < count; n++) {
    if (at + 46 > bytes.length || view.getUint32(at, true) !== CENTRAL) throw notEpub("ZIP corrompido");
    const nameLen = view.getUint16(at + 28, true);
    const name = decoder.decode(bytes.subarray(at + 46, at + 46 + nameLen));
    entries.set(name, {
      method: view.getUint16(at + 10, true),
      size: view.getUint32(at + 20, true),
      offset: view.getUint32(at + 42, true),
    });
    at += 46 + nameLen + view.getUint16(at + 30, true) + view.getUint16(at + 32, true);
  }
  return { bytes, view, entries };
}

async function inflateRaw(data) {
  const stream = new DecompressionStream("deflate-raw");
  const writer = stream.writable.getWriter();
  writer.write(data).catch(() => {});
  writer.close().catch(() => {});
  const reader = stream.readable.getReader();
  const chunks = [];
  let total = 0;
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    chunks.push(value);
    total += value.length;
  }
  const out = new Uint8Array(total);
  let pos = 0;
  for (const chunk of chunks) {
    out.set(chunk, pos);
    pos += chunk.length;
  }
  return out;
}

// readText devolve o conteúdo do arquivo do ZIP como texto (null se não há).
async function readText(zip, name) {
  let entry = zip.entries.get(name);
  if (!entry) {
    const lower = name.toLowerCase(); // há EPUBs com caixa trocada nos nomes
    entry = [...zip.entries].find(([k]) => k.toLowerCase() === lower)?.[1];
  }
  if (!entry) return null;
  const { view, bytes } = zip;
  if (view.getUint32(entry.offset, true) !== LOCAL) throw notEpub("ZIP corrompido");
  const start = entry.offset + 30 + view.getUint16(entry.offset + 26, true) + view.getUint16(entry.offset + 28, true);
  const data = bytes.subarray(start, start + entry.size);
  let raw;
  if (entry.method === 0) raw = data;
  else if (entry.method === 8) raw = await inflateRaw(data);
  else throw notEpub(`compressão ${entry.method} não suportada`);
  return new TextDecoder().decode(raw);
}

const ENTITIES = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };

function decodeXml(text) {
  return text
    .replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, "$1")
    .replace(/<[^>]+>/g, "")
    .replace(/&(#x[0-9a-f]+|#\d+|\w+);/gi, (m, e) =>
      e[0] === "#"
        ? String.fromCodePoint(e[1].toLowerCase() === "x" ? parseInt(e.slice(2), 16) : parseInt(e.slice(1), 10))
        : (ENTITIES[e] ?? m));
}

const collapse = (s) => s.replace(/[\s ]+/g, " ").trim();

// dcValues: os valores de <dc:tag> (com ou sem prefixo) dentro do metadata.
function dcValues(opf, tag) {
  const metadata = opf.match(/<(?:[\w-]+:)?metadata\b[\s\S]*?<\/(?:[\w-]+:)?metadata>/i)?.[0] ?? opf;
  const re = new RegExp(`<(?:[\\w-]+:)?${tag}\\b[^>]*>([\\s\\S]*?)</(?:[\\w-]+:)?${tag}>`, "gi");
  return [...metadata.matchAll(re)].map((m) => collapse(decodeXml(m[1]))).filter(Boolean);
}

/**
 * Confere o EPUB e lê título e autor. Lança com a mensagem para o usuário:
 * PDF/MOBI pelo nome, não-ZIP, DRM, pacote ausente.
 * @param {Uint8Array} bytes
 * @param {string} fileName  o título cai no nome do arquivo se o OPF não tiver
 */
export async function readEpubInfo(bytes, fileName = "") {
  const refused = checkBookName(fileName);
  if (refused) throw new Error(refused);
  const zip = readZip(bytes);

  if (zip.entries.has("META-INF/rights.xml")) throw new Error("o EPUB tem DRM (conteúdo cifrado) — o pnn só lê EPUB sem DRM");
  const encryption = await readText(zip, "META-INF/encryption.xml");
  if (encryption) {
    const algorithms = [...encryption.matchAll(/Algorithm="([^"]+)"/g)].map((m) => m[1]);
    // EncryptionMethod é o que cifra; os Algorithm de digest/KeyInfo não contam
    const methods = [...encryption.matchAll(/<(?:[\w-]+:)?EncryptionMethod\b[^>]*Algorithm="([^"]+)"/g)].map((m) => m[1]);
    if ((methods.length ? methods : algorithms).some((a) => !FONT_OBFUSCATION.has(a))) {
      throw new Error("o EPUB tem DRM (conteúdo cifrado) — o pnn só lê EPUB sem DRM");
    }
  }

  const container = await readText(zip, "META-INF/container.xml");
  const opfPath = container?.match(/<(?:[\w-]+:)?rootfile\b[^>]*full-path="([^"]+)"/)?.[1];
  if (!opfPath) throw notEpub("falta o META-INF/container.xml");
  const opf = await readText(zip, decodeURIComponent(opfPath));
  if (!opf) throw notEpub(`falta ${opfPath}`);

  const fallback = String(fileName).replace(/^.*[\\/]/, "").replace(/\.[^.]+$/, "");
  return {
    title: dcValues(opf, "title")[0] || fallback,
    author: dcValues(opf, "creator").join(", "),
  };
}
