// Monta um ZIP em memória para os testes (o EPUB é um ZIP): entradas
// guardadas (método 0) ou comprimidas com deflate (método 8), com CRC-32 e o
// diretório central — o bastante para ui/epub.js ler como leria um arquivo
// de verdade.

import { deflateRawSync, crc32 } from "node:zlib";

export function buildZip(entries) {
  const enc = new TextEncoder();
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const [name, content, { store = false } = {}] of entries) {
    const nameBytes = enc.encode(name);
    const raw = typeof content === "string" ? enc.encode(content) : content;
    const data = store ? raw : deflateRawSync(raw);
    const method = store ? 0 : 8;
    const crc = crc32(raw);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(method, 8);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(raw.length, 22);
    local.writeUInt16LE(nameBytes.length, 26);
    locals.push(local, nameBytes, data);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(method, 10);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(raw.length, 24);
    central.writeUInt16LE(nameBytes.length, 28);
    central.writeUInt32LE(offset, 42);
    centrals.push(central, nameBytes);
    offset += 30 + nameBytes.length + data.length;
  }
  const centralSize = centrals.reduce((n, b) => n + b.length, 0);
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(entries.length, 8);
  eocd.writeUInt16LE(entries.length, 10);
  eocd.writeUInt32LE(centralSize, 12);
  eocd.writeUInt32LE(offset, 16);
  return new Uint8Array(Buffer.concat([...locals, ...centrals, eocd]));
}

// epubZip: um EPUB mínimo com o OPF dado (e entradas extras).
export function epubZip(opf, extra = []) {
  return buildZip([
    ["mimetype", "application/epub+zip", { store: true }],
    ["META-INF/container.xml", `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`],
    ["OEBPS/content.opf", opf],
    ["OEBPS/c1.xhtml", "<html><body><p>texto</p></body></html>"],
    ...extra,
  ]);
}
