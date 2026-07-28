// Configuração do sync, resolvida por uma cadeia — o espelho do que a CLI faz
// com as variáveis de ambiente e o ~/.aws/credentials.
//
//   1. chrome.storage.managed  — política do navegador; é por aqui que a chave
//      chega provisionada de fora, sem ninguém digitar nada (arquivo JSON em
//      /etc/opt/chrome/policy/managed/ no Linux, registro no Windows).
//   2. chrome.storage.local    — gravado por script; sobrepõe a política, como
//      as variáveis de ambiente sobrepõem o perfil na CLI.
//
// A chave nunca é versionada: não existe valor embutido no código. O
// repositório é público, e chave AWS em repositório público é encontrada por
// varredura automática em minutos.

export const CONFIG_KEY = "sync_config";

const FIELDS = [
  "bucket",
  "prefix",
  "endpoint",
  "region",
  "accessKeyId",
  "secretAccessKey",
  "sessionToken",
];

export class SyncNotConfigured extends Error {
  constructor(missing) {
    super(
      `sync não configurado — falta ${missing.join(", ")}. ` +
        `Defina por política do navegador (storage.managed) ou grave em ` +
        `chrome.storage.local sob "${CONFIG_KEY}".`,
    );
    this.name = "SyncNotConfigured";
    this.missing = missing;
  }
}

async function readArea(area) {
  if (!area) return {};
  try {
    const stored = await area.get(CONFIG_KEY);
    // Na política, os campos podem vir soltos no topo (é o formato natural de
    // um JSON de policy) ou aninhados sob a mesma chave dos locais.
    const nested = stored?.[CONFIG_KEY] ?? {};
    const flat = await area.get(FIELDS);
    return { ...flat, ...nested };
  } catch {
    return {}; // área ausente (sem política instalada) não é erro
  }
}

/**
 * Resolve a configuração. Lança SyncNotConfigured quando falta o essencial.
 * @param {{managed?, local?}} areas  injetável para teste
 */
export async function loadSyncConfig({
  managed = globalThis.chrome?.storage?.managed,
  local = globalThis.chrome?.storage?.local,
} = {}) {
  const merged = { ...(await readArea(managed)), ...(await readArea(local)) };

  const config = {};
  for (const field of FIELDS) {
    const value = merged[field];
    if (typeof value === "string" && value.trim()) config[field] = value.trim();
  }

  const missing = ["bucket", "accessKeyId", "secretAccessKey"].filter((f) => !config[f]);
  if (missing.length) throw new SyncNotConfigured(missing);

  config.prefix = config.prefix ?? "pnn/";
  if (!config.prefix.endsWith("/")) config.prefix += "/";
  // Sem região explícita, us-east-1: diferente da CLI, o navegador não tem
  // perfil da AWS para consultar. Serviços compatíveis aceitam qualquer uma.
  config.region = config.region ?? "us-east-1";
  return config;
}
