// Adaptador de bucket S3 para a extensão — as mesmas três operações que o
// lado Go usa (list/get/put), sobre fetch + SigV4.
//
// Duas restrições do ambiente moldam este arquivo:
//
// 1. O service worker do Manifest V3 não tem DOM, então não há `DOMParser`
//    para ler o XML do ListObjectsV2. A resposta é estreita e conhecida, então
//    um extrator por expressão regular resolve sem dependência.
// 2. O `host` não pode ser definido em fetch; quem assina precisa saber disso
//    (ver sigv4.js).

import { signRequest } from "./sigv4.js";

const XML_ENTITIES = {
  "&quot;": '"',
  "&apos;": "'",
  "&lt;": "<",
  "&gt;": ">",
  "&amp;": "&",
};

// &amp; por último não adianta: decodificar em uma passada evita que
// "&amp;quot;" vire aspas.
function decodeXml(text) {
  return text.replace(/&(?:quot|apos|lt|gt|amp);/g, (entity) => XML_ENTITIES[entity]);
}

function tagValue(block, tag) {
  const match = block.match(new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`));
  return match ? decodeXml(match[1]) : "";
}

// parseList extrai { objects, nextToken } de uma resposta ListObjectsV2.
export function parseList(xml) {
  const objects = [];
  for (const [, block] of xml.matchAll(/<Contents>([\s\S]*?)<\/Contents>/g)) {
    const key = tagValue(block, "Key");
    if (key) objects.push({ key, etag: tagValue(block, "ETag") });
  }
  const truncated = tagValue(xml, "IsTruncated") === "true";
  return { objects, nextToken: truncated ? tagValue(xml, "NextContinuationToken") : "" };
}

/**
 * Cria o bucket. `config` traz bucket, region, endpoint? e as credenciais.
 * Devolve o mesmo contrato do lado Go: list/get/put.
 */
export function s3Bucket(config, { fetchImpl = fetch } = {}) {
  const { bucket, region, endpoint, accessKeyId, secretAccessKey, sessionToken } = config;
  const credentials = { accessKeyId, secretAccessKey, sessionToken };

  // Com endpoint próprio o endereçamento vai por caminho (host/bucket/chave):
  // serviços compatíveis raramente têm o DNS por subdomínio da AWS.
  const base = endpoint
    ? `${endpoint.replace(/\/$/, "")}/${bucket}`
    : `https://${bucket}.s3.${region}.amazonaws.com`;

  async function send(method, url, body = "") {
    const headers = await signRequest({ method, url, body, credentials, region });
    const response = await fetchImpl(url, {
      method,
      headers,
      body: method === "PUT" ? body : undefined,
    });
    if (!response.ok) {
      const detail = await response.text().catch(() => "");
      throw new Error(`S3 ${method} ${response.status}: ${tagValue(detail, "Message") || detail.slice(0, 200)}`);
    }
    return response;
  }

  return {
    async list(prefix) {
      const objects = [];
      let token = "";
      do {
        const url = new URL(base + "/");
        url.searchParams.set("list-type", "2");
        url.searchParams.set("prefix", prefix);
        if (token) url.searchParams.set("continuation-token", token);

        const page = parseList(await (await send("GET", url.toString())).text());
        objects.push(...page.objects);
        token = page.nextToken;
      } while (token);
      return objects;
    },

    async get(key) {
      return (await send("GET", `${base}/${key}`)).text();
    },

    async put(key, body) {
      const response = await send("PUT", `${base}/${key}`, body);
      return response.headers.get("ETag") ?? "";
    },
  };
}
