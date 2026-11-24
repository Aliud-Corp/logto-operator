// Edits or reads the throwaway Logto behind the operator's back, from inside its own Pod (kubectl exec, node -e).
// The m-default secret comes on stdin, never in argv.
//   node -e "$(cat logto.mjs)" drift <application name>   changes the application's description
//   node -e "$(cat logto.mjs)" exists <application name>  fails unless the application exists
const [action, name] = process.argv.slice(1);
const admin = "http://logto.logto.svc:3002";
const tenant = "http://logto.logto.svc:3001";

let secret = "";
for await (const chunk of process.stdin) secret += chunk;
secret = secret.trim();

const auth = Buffer.from(`m-default:${secret}`).toString("base64");
const tokenAnswer = await fetch(`${admin}/oidc/token`, {
  method: "POST",
  headers: { authorization: `Basic ${auth}`, "content-type": "application/x-www-form-urlencoded" },
  body: new URLSearchParams({ grant_type: "client_credentials", resource: "https://default.logto.app/api", scope: "all" }),
});
if (!tokenAnswer.ok) throw new Error(`token: ${tokenAnswer.status}`);
const { access_token: token } = await tokenAnswer.json();
const api = (path, init = {}) =>
  fetch(`${tenant}/api${path}`, { ...init, headers: { authorization: `Bearer ${token}`, "content-type": "application/json" } });

const list = await (await api("/applications?page_size=100")).json();
const app = list.find((a) => a.name === name);
switch (action) {
  case "exists":
    if (!app) throw new Error(`no application named ${name}`);
    console.log(`application ${name} exists`);
    break;
  case "drift": {
    if (!app) throw new Error(`no application named ${name}`);
    const answer = await api(`/applications/${app.id}`, {
      method: "PATCH",
      body: JSON.stringify({ description: "changed behind the operator's back" }),
    });
    if (!answer.ok) throw new Error(`patch: ${answer.status}`);
    console.log(`application ${name} changed`);
    break;
  }
  default:
    throw new Error(`unknown action ${action}`);
}
