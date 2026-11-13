// Sincronização por bucket S3 (spec/SPEC.md §5) — o mesmo algoritmo da CLI
// Go (`cli/internal/sync/sync.go`), sobre o EventLog da extensão.
//
// O layout é o que dispensa servidor: `<prefixo>/<device>.jsonl[.zst]`, um
// objeto por dispositivo, e cada dispositivo escreve SOMENTE o seu. Como dois
// clientes nunca gravam a mesma chave, não existe escrita perdida — e a
// extensão e a CLI são, para o bucket, apenas dois dispositivos quaisquer.
//
// A CLI sobe o objeto comprimido com zstd (.jsonl.zst): o log guarda o texto
// inteiro de cada versão de página, e a compressão acha as repetições. A
// extensão lê os dois formatos — reconhecidos pelo conteúdo, não pelo nome —
// e sobe o seu sem comprimir: é pequeno, e descomprimir é o que basta aqui.

import { decompress } from "../vendor/fzstd.js";

/**
 * Um ciclo completo: baixa o que mudou, funde, sobe o que é meu.
 *
 * @param {EventLog} log
 * @param {{list,get,put}} bucket
 * @param {string} prefix  terminado em "/"
 * @returns {Promise<{sent:number, received:number}>}
 */
export async function sync(log, bucket, prefix) {
  const device = await log.deviceId();
  const local = await log.events();
  const cursor = normalizeCursor(await log.getCursor(), local.length);

  const objects = await bucket.list(prefix);

  // ▼ Baixa o que mudou desde a última vez — inclusive o próprio objeto, que é
  // o que permite restaurar o log inteiro num perfil novo.
  const incoming = [];
  const etags = {};
  const key = `${prefix}${device}.jsonl`;
  let ownRemote = 0; // eventos meus no meu objeto, se ele foi baixado agora
  for (const object of objects) {
    if (!isLogKey(object.key)) continue; // o bucket pode ter outra coisa
    if (object.etag && cursor.etags[object.key] === object.etag) {
      etags[object.key] = object.etag;
      continue;
    }
    const events = parseLog(decodeObject(await bucket.get(object.key)));
    if (object.key === key) ownRemote = events.filter((e) => e.device === device).length;
    incoming.push(...events);
    etags[object.key] = object.etag;
  }

  const received = await log.merge(incoming);

  // ▲ Sobe só os eventos que este dispositivo autorou.
  const mine = (await log.events()).filter((event) => event.device === device);
  let sent = Math.max(mine.length - cursor.pushed, 0);

  // O objeto é sobrescrito inteiro: com menos eventos, perderia os que só
  // estão nele. Não acontece com a fusão acima — é a última guarda.
  if (mine.length < ownRemote) {
    throw new Error(`o objeto deste navegador no bucket tem ${ownRemote} eventos e o log só ${mine.length} — não sobrescrevo`);
  }
  // Sobe quando há evento novo meu — ou quando meu objeto sumiu do bucket
  // (apagado à mão), caso em que o cursor mente ao dizer que já foi enviado.
  if (mine.length > 0 && (sent > 0 || !(key in etags))) {
    etags[key] = await bucket.put(key, encodeLog(mine));
    cursor.pushed = mine.length;
  } else {
    sent = 0;
  }

  await log.setCursor({ etags, pushed: cursor.pushed });
  return { sent, received };
}

/**
 * Confere que o objeto deste dispositivo no bucket tem todos os eventos que
 * ele emitiu — a condição para "Sair deste dispositivo" apagar o log (SPEC
 * §5, Login e logout). Lança se faltar algum; devolve quantos conferiu.
 */
export async function verifyOwnObject(log, bucket, prefix) {
  const device = await log.deviceId();
  const mine = (await log.events()).filter((event) => event.device === device);
  if (!mine.length) return 0;
  const remote = new Set(parseLog(decodeObject(await bucket.get(`${prefix}${device}.jsonl`))).map((e) => e.id));
  const missing = mine.filter((event) => !remote.has(event.id)).length;
  if (missing) throw new Error(`${missing} de ${mine.length} eventos deste navegador não estão no bucket`);
  return mine.length;
}

/**
 * A identidade do bucket para a trava réplica ↔ bucket — a mesma forma da
 * CLI (sync.Config.Remote): endpoint, bucket e prefixo, normalizados.
 */
export function remoteOf({ endpoint = "", bucket, prefix = "pnn/" }) {
  const host = String(endpoint).trim().replace(/\/+$/, "").toLowerCase().replace(/^https?:\/\//, "") || "aws";
  return `s3://${host}/${bucket}/${prefix}`;
}

// O cursor é otimização, nunca fonte de verdade: perdido ou errado, o pior que
// acontece é rebaixar o que já se tem, e o dedup por id absorve. Com o log
// local vazio ele é descartado de propósito — senão um perfil recém-instalado
// acharia que está em dia e ficaria vazio para sempre.
function normalizeCursor(raw, localEvents) {
  if (!raw || localEvents === 0) return { etags: {}, pushed: 0 };
  return { etags: raw.etags ?? {}, pushed: raw.pushed ?? 0 };
}

const isLogKey = (key) => key.endsWith(".jsonl") || key.endsWith(".jsonl.zst");

// Todo frame zstd começa com 28 B5 2F FD (RFC 8878).
const ZSTD_MAGIC = [0x28, 0xb5, 0x2f, 0xfd];

// O JSONL de um objeto do bucket, comprimido ou não (texto também é aceito,
// para buckets em memória). Um zstd ilegível vira "" — o objeto é pulado sem
// derrubar o sync, como a linha corrompida no parseLog.
export function decodeObject(body) {
  if (typeof body === "string") return body;
  const bytes = body instanceof Uint8Array ? body : new Uint8Array(body);
  if (ZSTD_MAGIC.every((b, i) => bytes[i] === b)) {
    try {
      return new TextDecoder().decode(decompress(bytes));
    } catch {
      return "";
    }
  }
  return new TextDecoder().decode(bytes);
}

// Linha ilegível é pulada, não derruba o sync: um objeto truncado por um
// upload interrompido não pode impedir o resto de chegar.
export function parseLog(text) {
  const events = [];
  for (const line of text.split("\n")) {
    if (!line.trim()) continue;
    try {
      const event = JSON.parse(line);
      if (event && event.id) events.push(event);
    } catch {
      // linha corrompida: ignora
    }
  }
  return events;
}

export function encodeLog(events) {
  return events.map((event) => JSON.stringify(event)).join("\n") + "\n";
}
