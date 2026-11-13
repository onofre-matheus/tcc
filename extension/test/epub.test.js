// O cadastro de livro pela extensão confere o EPUB e lê título e autor do
// próprio arquivo, com as mesmas recusas da CLI — sem dependência (o ZIP é
// lido à mão e o deflate sai do DecompressionStream).

import { describe, expect, it } from "vitest";
import { readEpubInfo, checkBookName } from "../ui/epub.js";
import { buildZip, epubZip } from "./zip-helper.js";

const OPF = `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title id="t"> Mastering Your
      Adult ADHD &amp; mais </dc:title>
    <dc:title>Subtítulo ignorado</dc:title>
    <dc:creator>Steven A. Safren</dc:creator>
    <dc:creator opf:role="aut">Susan E. Sprich</dc:creator>
  </metadata>
  <manifest/><spine/>
</package>`;

describe("readEpubInfo", () => {
  it("lê título e autores do OPF (deflate, prefixo dc:, entidades, espaços)", async () => {
    const info = await readEpubInfo(epubZip(OPF), "qualquer.epub");
    expect(info).toEqual({ title: "Mastering Your Adult ADHD & mais", author: "Steven A. Safren, Susan E. Sprich" });
  });

  it("sem título no OPF, vale o nome do arquivo", async () => {
    const info = await readEpubInfo(epubZip(`<package><metadata></metadata></package>`), "/x/Meu Livro.epub");
    expect(info).toEqual({ title: "Meu Livro", author: "" });
  });

  it("recusa DRM, mas aceita fonte ofuscada", async () => {
    const drm = `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`;
    await expect(readEpubInfo(epubZip(OPF, [["META-INF/encryption.xml", drm]]), "a.epub")).rejects.toThrow("DRM");
    const font = `<encryption><enc:EncryptedData><enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/></enc:EncryptedData></encryption>`;
    await expect(readEpubInfo(epubZip(OPF, [["META-INF/encryption.xml", font]]), "a.epub")).resolves.toMatchObject({ author: "Steven A. Safren, Susan E. Sprich" });
    await expect(readEpubInfo(epubZip(OPF, [["META-INF/rights.xml", "<rights/>"]]), "a.epub")).rejects.toThrow("DRM");
  });

  it("recusa o que não é EPUB, com as mensagens da CLI", async () => {
    await expect(readEpubInfo(new TextEncoder().encode("%PDF-1.7 ..."), "livro.pdf")).rejects.toThrow("não PDF");
    await expect(readEpubInfo(new Uint8Array(10), "livro.mobi")).rejects.toThrow("ebook-convert");
    await expect(readEpubInfo(new TextEncoder().encode("não é zip nenhum, só texto qualquer"), "x.epub")).rejects.toThrow("não é um EPUB válido");
    await expect(readEpubInfo(buildZip([["a.txt", "oi"]]), "x.epub")).rejects.toThrow("container.xml");
    expect(checkBookName("ok.epub")).toBeNull();
  });
});
