// "Sair deste dispositivo" (SPEC §5, Login e logout): sincroniza, CONFERE que
// o bucket tem tudo o que este navegador emitiu e só então apaga log,
// credencial e alarmes — qualquer falha antes da conferência não apaga nada.
// E a trava réplica ↔ bucket: o log que sincronizou com um bucket não fala
// com outro.

import { beforeEach, describe, expect, it, vi } from "vitest";

const storage = new Map();
const clearAll = vi.fn(async () => true);
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
      async clear() {
        storage.clear();
      },
    },
    managed: undefined,
    onChanged: { addListener() {} },
  },
  alarms: { clearAll },
};

const store = await import("../ui/store.js");

function memBucket({ failPut = false, truncate = false } = {}) {
  const objects = new Map();
  return {
    objects,
    async list(prefix) {
      return [...objects.keys()].filter((k) => k.startsWith(prefix)).map((key) => ({ key, etag: String(objects.get(key).length) }));
    },
    async get(key) {
      return objects.get(key) ?? "";
    },
    async put(key, body) {
      if (failPut) throw new Error("S3 PUT 503: indisponível");
      objects.set(key, truncate ? body.slice(0, Math.floor(body.length / 2)) : body);
      return "etag";
    },
  };
}

const config = { bucket: "meu-bucket", prefix: "pnn/" };

beforeEach(async () => {
  storage.clear();
  clearAll.mockClear();
  await store.createTask("Tarefa do trabalho");
  await store.createTask("Outra tarefa");
});

describe("Sair deste dispositivo", () => {
  it("confere e apaga tudo: log, credencial, configuração, alarmes", async () => {
    storage.set("sync_config", { bucket: "meu-bucket", accessKeyId: "AKIA", secretAccessKey: "segredo" });
    const bucket = memBucket();
    const { verified } = await store.logout({ config, bucket });
    expect(verified).toBe(2);
    expect(storage.size).toBe(0);
    expect(clearAll).toHaveBeenCalled();
    // o bucket tem os dois eventos — o login de amanhã os traz de volta
    const [key] = [...bucket.objects.keys()];
    expect(bucket.objects.get(key).split("\n").filter(Boolean)).toHaveLength(2);
  });

  it("envio que falha: nada é apagado", async () => {
    await expect(store.logout({ config, bucket: memBucket({ failPut: true }) })).rejects.toThrow("503");
    expect(await store.log.events()).toHaveLength(2);
    expect(clearAll).not.toHaveBeenCalled();
  });

  it("o bucket guardou pela metade: a conferência pega e nada é apagado", async () => {
    await expect(store.logout({ config, bucket: memBucket({ truncate: true }) })).rejects.toThrow("não estão no bucket");
    expect(await store.log.events()).toHaveLength(2);
  });

  it("sem configuração de sync, não há como conferir: nada é apagado", async () => {
    await expect(store.logout()).rejects.toThrow("sync não configurado");
    expect(await store.log.events()).toHaveLength(2);
  });
});

describe("trava réplica ↔ bucket", () => {
  it("o log que sincronizou com um bucket não fala com outro", async () => {
    await store.syncNow({ config, bucket: memBucket() });
    expect(storage.get("sync_remote")).toBe("s3://aws/meu-bucket/pnn/");
    const other = memBucket();
    await expect(store.syncNow({ config: { bucket: "outro", prefix: "pnn/" }, bucket: other })).rejects.toThrow("pertence a s3://aws/meu-bucket/pnn/");
    expect(other.objects.size).toBe(0);
  });
});
