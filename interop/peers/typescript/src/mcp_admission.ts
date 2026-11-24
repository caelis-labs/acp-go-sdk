import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import * as v1 from "@agentclientprotocol/sdk/unstable";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2/unstable";

const version = Number(process.argv[2]);
assert(version === 1 || version === 2);
const sdk = version === 1 ? v1 : v2;
const provider = new sdk.McpOverAcpProvider();
let childAnnounced = false;
let runs = 0;

// The same fixture uses the explicit unstable client of each wire profile.
const app: any = sdk.client();
const conn = app
  .onRequest("mcp/message", provider.handleRequest)
  .onNotification("session/update", async ({ params, agent }: any) => {
    if (params.sessionId === "parent" &&
        params.update.sessionUpdate === "subagent_update" &&
        params.update.sessionId === "child") {
      childAnnounced = true;
      await agent.notify("_interop/child-observed", {});
    }
  })
  .connect(sdk.ndJsonStream(Writable.toWeb(process.stdout), Readable.toWeb(process.stdin)));

const init = await conn.agent.request("initialize", version === 1
  ? { protocolVersion: 1, clientCapabilities: { subagents: {} } }
  : { protocolVersion: 2, info: { name: "mcp-admission-fixture", version: "1" } },
);
if (version === 1) assert.equal(init.agentCapabilities?.mcpCapabilities?.acp, true);
else assert.deepEqual(init.capabilities?.session?.mcp?.acp, {});
const server = provider.registerServer(conn.agent, {
  name: "attribution-fixture",
  admit(request) {
    if (request.requireSession() !== "child" || !childAnnounced) {
      request.rejectSession("outside the fixture session");
    }
  },
  handler({ sessionId, toolCallId }) {
    return { result: { sessionId: sessionId ?? null, toolCallId: toolCallId ?? null, runs: ++runs } };
  },
});
const session = await conn.agent.request("session/new", { cwd: "/tmp", mcpServers: [server.declaration] });
assert.equal(session.sessionId, "parent");
await conn.agent.notify("_interop/mcp-ready", {});
await conn.closed;
