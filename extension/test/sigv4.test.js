// Conformidade da assinatura SigV4 contra o signer OFICIAL da AWS.
//
// Os vetores em sigv4_vectors.json foram gerados pelo
// `aws-sdk-go-v2/aws/signer/v4` com insumos fixos (credencial de exemplo da
// documentação, instante congelado). Se a implementação daqui divergir em uma
// vírgula da canonicalização, a assinatura muda por inteiro e o teste quebra —
// que é exatamente o erro impossível de diagnosticar contra a AWS de verdade,
// onde ele aparece só como um 403 SignatureDoesNotMatch.

import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { signRequest } from "../storage/sigv4.js";

const vectors = JSON.parse(
  readFileSync(fileURLToPath(new URL("./sigv4_vectors.json", import.meta.url)), "utf8"),
);

// Credencial de exemplo da documentação da AWS — não é segredo de ninguém.
const credentials = {
  accessKeyId: "AKIAIOSFODNN7EXAMPLE",
  secretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
};
const date = new Date("2026-07-28T01:45:00Z");

describe("SigV4 confere com o signer oficial", () => {
  it.each(vectors.map((v) => [v.name, v]))("%s", async (_name, vector) => {
    const headers = await signRequest({
      method: vector.method,
      url: vector.url,
      body: vector.body,
      region: vector.region,
      date,
      credentials: { ...credentials, sessionToken: vector.token || undefined },
    });
    expect(headers.Authorization).toBe(vector.expect);
  });

  it("não devolve host — o navegador o define e proíbe sobrescrever", async () => {
    const headers = await signRequest({
      method: "GET",
      url: "https://meu-bucket.s3.us-east-1.amazonaws.com/pnn/a.jsonl",
      credentials,
      region: "us-east-1",
      date,
    });
    expect(headers.host).toBeUndefined();
    // ...mas host entra na assinatura, senão a AWS recusa.
    expect(headers.Authorization).toContain("SignedHeaders=host;");
  });

  it("manda o token de sessão como cabeçalho quando a credencial é temporária", async () => {
    const headers = await signRequest({
      method: "GET",
      url: "https://meu-bucket.s3.us-east-1.amazonaws.com/pnn/a.jsonl",
      credentials: { ...credentials, sessionToken: "token-abc" },
      region: "us-east-1",
      date,
    });
    expect(headers["x-amz-security-token"]).toBe("token-abc");
    expect(headers.Authorization).toContain("x-amz-security-token");
  });
});
