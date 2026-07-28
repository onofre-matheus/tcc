// Assinatura AWS Signature Version 4 (SigV4) com Web Crypto.
//
// A extensão não tem bundler — os módulos são carregados direto pelo
// navegador —, então trazer o SDK da AWS custaria um passo de build e centenas
// de kB para usar três operações. O protocolo em si cabe aqui: derivar a chave
// de assinatura é uma cadeia de quatro HMAC-SHA256, e `crypto.subtle` faz
// HMAC-SHA256 nativamente.
//
// Só `host` e os cabeçalhos `x-amz-*` são assinados. `content-length` fica de
// fora de propósito: o fetch do navegador proíbe defini-lo, e a SigV4 não o
// exige — assina-se o conjunto que se declara em SignedHeaders. É o mesmo que
// o SDK oficial da AWS para JavaScript faz quando roda no navegador.
//
// A conformidade é verificada contra o signer oficial do SDK Go em
// test/sigv4_vectors.json — mesmos insumos, mesma assinatura, byte a byte.
//
// Referência: docs.aws.amazon.com/.../sigv4-create-canonical-request.html

const encoder = new TextEncoder();
const ALGORITHM = "AWS4-HMAC-SHA256";
const SERVICE = "s3";

async function sha256Hex(data) {
  const bytes = typeof data === "string" ? encoder.encode(data) : data;
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return hex(new Uint8Array(digest));
}

async function hmac(key, message) {
  const cryptoKey = await crypto.subtle.importKey(
    "raw",
    typeof key === "string" ? encoder.encode(key) : key,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signature = await crypto.subtle.sign("HMAC", cryptoKey, encoder.encode(message));
  return new Uint8Array(signature);
}

function hex(bytes) {
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// A AWS exige RFC 3986: encodeURIComponent deixa !'()* de fora.
function uriEncode(value) {
  return encodeURIComponent(value).replace(
    /[!'()*]/g,
    (c) => "%" + c.charCodeAt(0).toString(16).toUpperCase(),
  );
}

// O caminho é codificado segmento a segmento — a barra separa e não se escapa.
function canonicalPath(pathname) {
  return pathname.split("/").map(uriEncode).join("/") || "/";
}

// Parâmetros ordenados por nome, no formato canônico.
function canonicalQuery(searchParams) {
  return [...searchParams.entries()]
    .map(([k, v]) => [uriEncode(k), uriEncode(v)])
    .sort((a, b) => (a[0] === b[0] ? (a[1] < b[1] ? -1 : 1) : a[0] < b[0] ? -1 : 1))
    .map(([k, v]) => `${k}=${v}`)
    .join("&");
}

// amzDate = 20260728T014500Z; dateStamp = 20260728
function timestamps(date) {
  const amzDate = date.toISOString().replace(/[:-]|\.\d{3}/g, "");
  return { amzDate, dateStamp: amzDate.slice(0, 8) };
}

/**
 * Assina uma requisição e devolve os cabeçalhos a enviar.
 *
 * O cabeçalho `host` entra na assinatura mas NÃO é devolvido: o navegador o
 * define sozinho e proíbe sobrescrevê-lo — por isso o valor assinado tem de
 * ser exatamente `url.host` (com porta, quando houver).
 *
 * @param {object} req  { method, url, body?, credentials, region }
 * @returns {Promise<Record<string,string>>} cabeçalhos assinados
 */
export async function signRequest({ method, url, body = "", credentials, region, date = new Date() }) {
  const { accessKeyId, secretAccessKey, sessionToken } = credentials;
  const { amzDate, dateStamp } = timestamps(date);
  const target = new URL(url);
  const payloadHash = await sha256Hex(body);

  const headers = {
    host: target.host,
    "x-amz-content-sha256": payloadHash,
    "x-amz-date": amzDate,
  };
  if (sessionToken) headers["x-amz-security-token"] = sessionToken;

  const names = Object.keys(headers).sort();
  const canonicalHeaders = names.map((n) => `${n}:${headers[n].trim()}\n`).join("");
  const signedHeaders = names.join(";");

  const canonicalRequest = [
    method,
    canonicalPath(target.pathname),
    canonicalQuery(target.searchParams),
    canonicalHeaders,
    signedHeaders,
    payloadHash,
  ].join("\n");

  const scope = `${dateStamp}/${region}/${SERVICE}/aws4_request`;
  const stringToSign = [
    ALGORITHM,
    amzDate,
    scope,
    await sha256Hex(canonicalRequest),
  ].join("\n");

  // Cadeia de derivação: data → região → serviço → aws4_request.
  let key = await hmac(`AWS4${secretAccessKey}`, dateStamp);
  key = await hmac(key, region);
  key = await hmac(key, SERVICE);
  key = await hmac(key, "aws4_request");
  const signature = hex(await hmac(key, stringToSign));

  const signed = { ...headers };
  delete signed.host; // o navegador manda; sobrescrever é proibido
  signed.Authorization =
    `${ALGORITHM} Credential=${accessKeyId}/${scope}, ` +
    `SignedHeaders=${signedHeaders}, Signature=${signature}`;
  return signed;
}
