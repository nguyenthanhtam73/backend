/**
 * One-shot: inspect / re-issue Railway SSL for api.dadiary.vn
 * Usage: node scripts/fix-api-tls.mjs [--reissue]
 *
 * Reads token from ~/.railway/config.json (Railway CLI login).
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import tls from "node:tls";

const PROJECT_ID = "d256f6f2-651d-4c8a-b880-95e19c9ce09c";
const ENV_ID = "f8bd8b45-1901-464e-9b2f-bb212297e1d1";
const SERVICE_ID = "6b46ebe3-6de5-4fb8-adfa-55c8e0bc8289";
const DOMAIN = "api.dadiary.vn";
const GQL = "https://backboard.railway.com/graphql/v2";

const reissue = process.argv.includes("--reissue");
const issueOnly = process.argv.includes("--issue");

function loadToken() {
  const cfgPath = path.join(os.homedir(), ".railway", "config.json");
  const cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
  const token = cfg?.user?.accessToken;
  if (!token) throw new Error("No Railway accessToken — run railway login");
  return token;
}

async function gql(token, query, variables) {
  const res = await fetch(GQL, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ query, variables }),
  });
  const json = await res.json();
  if (json.errors?.length) {
    throw new Error(JSON.stringify(json.errors, null, 2));
  }
  return json.data;
}

function checkCert(host) {
  return new Promise((resolve) => {
    const s = tls.connect(
      { host, port: 443, servername: host, rejectUnauthorized: false },
      () => {
        const c = s.getPeerCertificate();
        resolve({
          host,
          cn: c.subject?.CN,
          san: c.subjectaltname,
          valid_to: c.valid_to,
        });
        s.end();
      },
    );
    s.on("error", (e) => resolve({ host, err: e.message }));
  });
}

async function getCustomDomain(token) {
  const data = await gql(
    token,
    `query($id: String!) {
      project(id: $id) {
        environments {
          edges {
            node {
              id
              serviceInstances {
                edges {
                  node {
                    serviceId
                    domains {
                      customDomains {
                        id
                        domain
                        status {
                          certificateStatus
                          cdnProvider
                          dnsRecords {
                            hostlabel
                            purpose
                            requiredValue
                            currentValue
                            status
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }`,
    { id: PROJECT_ID },
  );

  for (const env of data.project.environments.edges) {
    if (env.node.id !== ENV_ID) continue;
    for (const si of env.node.serviceInstances.edges) {
      if (si.node.serviceId !== SERVICE_ID) continue;
      return (
        si.node.domains.customDomains.find((d) => d.domain === DOMAIN) || null
      );
    }
  }
  return null;
}

async function deleteDomain(token, id) {
  // Try common mutation names used by Railway over time.
  const attempts = [
    {
      query: `mutation($id: String!) { customDomainDelete(id: $id) }`,
      variables: { id },
    },
    {
      query: `mutation($id: String!) { domainDelete(id: $id) }`,
      variables: { id },
    },
  ];
  let lastErr;
  for (const a of attempts) {
    try {
      const data = await gql(token, a.query, a.variables);
      return data;
    } catch (e) {
      lastErr = e;
    }
  }
  throw lastErr;
}

async function createDomain(token) {
  const attempts = [
    {
      query: `mutation($input: CustomDomainCreateInput!) {
        customDomainCreate(input: $input) {
          id
          domain
          status { certificateStatus }
        }
      }`,
      variables: {
        input: {
          domain: DOMAIN,
          environmentId: ENV_ID,
          serviceId: SERVICE_ID,
          projectId: PROJECT_ID,
          targetPort: 8080,
        },
      },
    },
    {
      query: `mutation($input: DomainCreateInput!) {
        domainCreate(input: $input) {
          id
          domain
        }
      }`,
      variables: {
        input: {
          domain: DOMAIN,
          environmentId: ENV_ID,
          serviceId: SERVICE_ID,
        },
      },
    },
  ];
  let lastErr;
  for (const a of attempts) {
    try {
      return await gql(token, a.query, a.variables);
    } catch (e) {
      lastErr = e;
    }
  }
  throw lastErr;
}

async function listDomainMutations(token) {
  const data = await gql(
    token,
    `{ __type(name: "Mutation") { fields { name args { name type { kind name ofType { kind name ofType { name } } } } } } }`,
  );
  return data.__type.fields.filter((n) =>
    /domain|cert|custom/i.test(n.name),
  );
}

async function issueCertificate(token, id) {
  // Prefer dedicated issue mutation; fall back to known arg shapes.
  const attempts = [
    {
      query: `mutation($id: String!) { customDomainIssueCertificate(id: $id) }`,
      variables: { id },
    },
    {
      query: `mutation($input: CustomDomainIssueCertificateInput!) {
        customDomainIssueCertificate(input: $input)
      }`,
      variables: { input: { id } },
    },
    {
      query: `mutation($id: String!) {
        customDomainIssueCertificate(customDomainId: $id)
      }`,
      variables: { id },
    },
  ];
  let lastErr;
  for (const a of attempts) {
    try {
      return await gql(token, a.query, a.variables);
    } catch (e) {
      lastErr = e;
    }
  }
  throw lastErr;
}

async function pollUntilIssued(token, { rounds = 40, delayMs = 15_000 } = {}) {
  for (let i = 0; i < rounds; i++) {
    await new Promise((r) => setTimeout(r, delayMs));
    const cur = await getCustomDomain(token);
    const cert = await checkCert(DOMAIN);
    console.log(
      `poll[${i}] certStatus=${cur?.status?.certificateStatus} cn=${cert.cn} san=${cert.san}`,
    );
    const ok =
      cur?.status?.certificateStatus === "CERTIFICATE_STATUS_TYPE_ISSUED" ||
      (cert.san && String(cert.san).includes(DOMAIN));
    if (ok) {
      console.log("SUCCESS", JSON.stringify({ cur, cert }, null, 2));
      return true;
    }
  }
  return false;
}

async function main() {
  const token = loadToken();
  const before = await getCustomDomain(token);
  console.log("STATUS_BEFORE", JSON.stringify(before, null, 2));
  console.log("CERT_BEFORE", JSON.stringify(await checkCert(DOMAIN), null, 2));

  if (!reissue && !issueOnly) {
    console.log(
      "\nDry-run only.\n  --issue   call customDomainIssueCertificate\n  --reissue delete+recreate domain",
    );
    console.log(
      "Domain mutations:",
      JSON.stringify(await listDomainMutations(token), null, 2),
    );
    return;
  }

  if (issueOnly) {
    if (!before?.id) throw new Error("custom domain not found");
    console.log(`Issuing certificate for ${before.id}…`);
    console.log(
      "ISSUE",
      JSON.stringify(await issueCertificate(token, before.id), null, 2),
    );
    const ok = await pollUntilIssued(token);
    if (!ok) {
      console.log("TIMEOUT after --issue; try --reissue next.");
      process.exitCode = 2;
    }
    return;
  }

  if (!before?.id) {
    console.log("Domain missing — creating…");
  } else {
    console.log(`Deleting custom domain ${before.id}…`);
    console.log(
      "DELETE",
      JSON.stringify(await deleteDomain(token, before.id), null, 2),
    );
    // Brief pause so Railway releases the binding before recreate.
    await new Promise((r) => setTimeout(r, 15_000));
  }

  console.log("Creating custom domain…");
  console.log("CREATE", JSON.stringify(await createDomain(token), null, 2));

  const ok = await pollUntilIssued(token);
  if (!ok) {
    console.log(
      "TIMEOUT — cert still not issued. Check Railway dashboard / PaVietnam DNS.",
    );
    process.exitCode = 2;
  }
}

main().catch((e) => {
  console.error("FATAL", e.message || e);
  process.exit(1);
});
