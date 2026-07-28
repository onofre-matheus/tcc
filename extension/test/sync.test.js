// Espelho da bateria do lado Go (cli/internal/sync/sync_test.go): o que
// precisa ser provado é convergência (SPEC §2) e idempotência. O bucket vira
// um mapa em memória e cada "dispositivo" é um EventLog sobre memoryKV.

import { describe, expect, it } from "vitest";

import { EventLog } from "../storage/log.js";
import { memoryKV } from "../storage/kv.js";
import { sync, parseLog, encodeLog } from "../storage/sync.js";
import { parseList } from "../storage/s3.js";
import { loadSyncConfig, SyncNotConfigured, CONFIG_KEY } from "../storage/sync_config.js";

function fakeBucket() {
  const objects = new Map();
  let puts = 0;
  return {
    objects,
    get puts() {
      return puts;
    },
    async list(prefix) {
      return [...objects.entries()]
        .filter(([key]) => key.startsWith(prefix))
        .map(([key, body]) => ({ key, etag: `"${body.length}"` }));
    },
    async get(key) {
      return objects.get(key) ?? "";
    },
    async put(key, body) {
      puts++;
      objects.set(key, body);
      return `"${body.length}"`;
    },
  };
}

const device = () => new EventLog(memoryKV());
const typesOf = async (log) => (await log.events()).map((e) => e.type).sort();

describe("sync por bucket", () => {
  // O caso que motiva o desenho: dois clientes emitem sem se ver e, depois de
  // sincronizar, os dois têm o log completo. Nenhum apaga o outro.
  it("faz dois dispositivos convergirem", async () => {
    const bucket = fakeBucket();
    const a = device();
    const b = device();
    await a.append("task.created", { task_id: "t1" });
    await b.append("card.created", { card_id: "c1" });

    expect(await sync(a, bucket, "pnn/")).toEqual({ sent: 1, received: 0 });
    expect(await sync(b, bucket, "pnn/")).toEqual({ sent: 1, received: 1 });
    expect(await sync(a, bucket, "pnn/")).toEqual({ sent: 0, received: 1 });

    expect(await typesOf(a)).toEqual(["card.created", "task.created"]);
    expect(await typesOf(b)).toEqual(["card.created", "task.created"]);
  });

  it("não faz nada quando não há novidade", async () => {
    const bucket = fakeBucket();
    const a = device();
    await a.append("task.created", { task_id: "t1" });
    await sync(a, bucket, "pnn/");
    const putsDepois = bucket.puts;

    for (let i = 0; i < 3; i++) {
      expect(await sync(a, bucket, "pnn/")).toEqual({ sent: 0, received: 0 });
    }
    expect(bucket.puts).toBe(putsDepois);
  });

  it("restaura um perfil novo a partir do bucket", async () => {
    const bucket = fakeBucket();
    const a = device();
    for (const id of ["t1", "t2", "t3"]) await a.append("task.created", { task_id: id });
    await sync(a, bucket, "pnn/");

    const novo = device();
    expect(await sync(novo, bucket, "pnn/")).toEqual({ sent: 0, received: 3 });
    expect((await novo.events()).length).toBe(3);
  });

  // É o que elimina a escrita perdida: o objeto de A não pode engordar com os
  // eventos de B depois que A sincroniza.
  it("faz cada dispositivo escrever somente a própria chave", async () => {
    const bucket = fakeBucket();
    const a = device();
    const b = device();
    await a.append("task.created", { task_id: "t1" });
    await b.append("card.created", { card_id: "c1" });
    await sync(a, bucket, "pnn/");
    await sync(b, bucket, "pnn/");
    await sync(a, bucket, "pnn/");
    await sync(a, bucket, "pnn/");

    expect(bucket.objects.size).toBe(2);
    for (const [key, body] of bucket.objects) {
      const events = parseLog(body);
      expect(events).toHaveLength(1);
      expect(key).toContain(events[0].device);
    }
  });

  it("sobrevive a objeto truncado no bucket", async () => {
    const bucket = fakeBucket();
    const a = device();
    await a.append("task.created", { task_id: "t1" });
    await sync(a, bucket, "pnn/");
    bucket.objects.set("pnn/dev-corrompido.jsonl", '{"id":"x","type":"tas');

    const novo = device();
    expect((await sync(novo, bucket, "pnn/")).received).toBe(1);
  });

  it("se recupera de cursor perdido sem duplicar", async () => {
    const bucket = fakeBucket();
    const a = device();
    const b = device();
    await b.append("card.created", { card_id: "c1" });
    await sync(b, bucket, "pnn/");
    await sync(a, bucket, "pnn/");

    await a.setCursor(null);
    await sync(a, bucket, "pnn/");
    expect((await a.events()).length).toBe(1);
  });

  // A extensão e a CLI são, para o bucket, dois dispositivos quaisquer: o que
  // a CLI gravou tem de entrar aqui sem tratamento especial.
  it("aceita o objeto gravado pela CLI Go", async () => {
    const bucket = fakeBucket();
    bucket.objects.set(
      "pnn/dev-cli-go.jsonl",
      '{"id":"0197c9a4-1","type":"task.created","v":1,"lc":7,' +
        '"ts":"2026-07-27T23:31:00.000Z","device":"dev-cli-go","payload":{"task_id":"t1"}}\n',
    );

    const extensao = device();
    expect((await sync(extensao, bucket, "pnn/")).received).toBe(1);
    expect((await extensao.events())[0].lc).toBe(7);

    // Lamport avança para max(local, recebidos): o próximo evento local não
    // pode nascer "antes" do que veio da CLI (SPEC §1).
    const proximo = await extensao.append("task.created", { task_id: "t2" });
    expect(proximo.lc).toBe(8);
  });

  it("escreve JSONL que o outro lado consegue ler de volta", () => {
    const events = [{ id: "e1", type: "a", payload: {} }, { id: "e2", type: "b", payload: {} }];
    expect(parseLog(encodeLog(events))).toEqual(events);
  });
});

describe("ListObjectsV2 sem DOMParser", () => {
  it("extrai chaves, ETags e a continuação", () => {
    const xml = `<?xml version="1.0"?><ListBucketResult>
      <IsTruncated>true</IsTruncated>
      <NextContinuationToken>tok123</NextContinuationToken>
      <Contents><Key>pnn/dev-a.jsonl</Key><ETag>&quot;abc&quot;</ETag><Size>10</Size></Contents>
      <Contents><Key>pnn/dev-b.jsonl</Key><ETag>&quot;def&quot;</ETag><Size>20</Size></Contents>
    </ListBucketResult>`;

    const { objects, nextToken } = parseList(xml);
    expect(objects).toEqual([
      { key: "pnn/dev-a.jsonl", etag: '"abc"' },
      { key: "pnn/dev-b.jsonl", etag: '"def"' },
    ]);
    expect(nextToken).toBe("tok123");
  });

  it("não pede continuação quando a listagem terminou", () => {
    const xml = `<ListBucketResult><IsTruncated>false</IsTruncated>
      <Contents><Key>pnn/dev-a.jsonl</Key><ETag>&quot;abc&quot;</ETag></Contents>
    </ListBucketResult>`;
    expect(parseList(xml).nextToken).toBe("");
  });
});

describe("configuração do sync", () => {
  const area = (data) => ({ async get(keys) {
    if (typeof keys === "string") return data[keys] !== undefined ? { [keys]: data[keys] } : {};
    const out = {};
    for (const k of keys) if (data[k] !== undefined) out[k] = data[k];
    return out;
  } });

  it("lê a política do navegador (chave provisionada de fora)", async () => {
    const config = await loadSyncConfig({
      managed: area({ bucket: "meu-bucket", accessKeyId: "AKIA", secretAccessKey: "s3cr3t" }),
      local: area({}),
    });
    expect(config.bucket).toBe("meu-bucket");
    expect(config.prefix).toBe("pnn/");
    expect(config.region).toBe("us-east-1");
  });

  it("deixa o local sobrepor a política, como o ambiente sobrepõe o perfil na CLI", async () => {
    const config = await loadSyncConfig({
      managed: area({ bucket: "bucket-da-politica", accessKeyId: "A", secretAccessKey: "s" }),
      local: area({ [CONFIG_KEY]: { bucket: "bucket-local", prefix: "estudos" } }),
    });
    expect(config.bucket).toBe("bucket-local");
    expect(config.prefix).toBe("estudos/");
  });

  it("diz exatamente o que falta", async () => {
    await expect(loadSyncConfig({ managed: area({}), local: area({ bucket: "b" }) }))
      .rejects.toThrow(SyncNotConfigured);
    await expect(loadSyncConfig({ managed: area({}), local: area({ bucket: "b" }) }))
      .rejects.toThrow(/accessKeyId/);
  });

  it("não quebra quando não há política instalada", async () => {
    const config = await loadSyncConfig({
      managed: undefined,
      local: area({ bucket: "b", accessKeyId: "A", secretAccessKey: "s" }),
    });
    expect(config.bucket).toBe("b");
  });
});
