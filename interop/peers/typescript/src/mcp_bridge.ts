import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import * as v1 from "@agentclientprotocol/sdk/unstable";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2/unstable";
import {
  McpOverAcpHttpBridge,
  type McpServerHttpDeclaration,
} from "@agentclientprotocol/sdk/experimental/mcp-http";

const version = Number(process.argv[2]);
assert(version === 1 || version === 2);
const sdk = version === 1 ? v1 : v2;
const caller = new sdk.McpOverAcpCaller();
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
const entered = deferred<AbortSignal>();
const announced = deferred<void>();
const settled = deferred<void>();
const bridge = new McpOverAcpHttpBridge(caller, {
  attribution: async ({ params, signal }) => {
    switch (params?.name) {
      case "waiting":
        entered.resolve(signal);
        await announced.promise;
        settled.resolve();
        return { sessionId: "child", toolCallId: "tool-1" };
      case "owner":
        return { sessionId: "child", toolCallId: "tool-1" };
      case "wrong-session":
        return { sessionId: "parent", toolCallId: "tool-1" };
      default:
        return undefined;
    }
  },
});

function post(server: McpServerHttpDeclaration, name: string, signal?: AbortSignal) {
  const protocolVersion = "2026-07-28";
  return fetch(server.url, {
    method: "POST",
    headers: {
      authorization: server.headers[0]!.value,
      accept: "application/json, text/event-stream",
      "content-type": "application/json",
      "mcp-protocol-version": protocolVersion,
      "mcp-method": "tools/call",
      "mcp-name": name,
    },
    signal,
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: name,
      method: "tools/call",
      params: {
        name,
        _meta: {
          "io.modelcontextprotocol/protocolVersion": protocolVersion,
          "io.modelcontextprotocol/clientCapabilities": {},
        },
      },
    }),
  });
}

async function exercise(server: McpServerHttpDeclaration, announce: () => Promise<void>) {
  const controller = new AbortController();
  const dropped = post(server, "waiting", controller.signal)
    .then(() => assert.fail("abandoned request was answered"), (error: Error) => {
      assert.equal(error.name, "AbortError");
    });
  const signal = await entered.promise;
  assert.equal(signal.aborted, false);
  const aborted = new Promise<void>((resolve) =>
    signal.addEventListener("abort", () => resolve(), { once: true }),
  );
  controller.abort();
  await Promise.all([dropped, aborted]);

  // Resolve the child lookup only after the bridge observed cancellation.
  await announce();
  announced.resolve();
  await settled.promise;

  for (const name of ["missing-session", "wrong-session"]) {
    const response = await post(server, name);
    assert.equal(response.status, 200);
    const body = await response.json() as { error?: { code: number } };
    assert.equal(body.error?.code, -33002);
  }
  const response = await post(server, "owner");
  assert.equal(response.status, 200);
  const body = await response.json() as { result?: unknown };
  assert.deepEqual(body.result, {
    sessionId: "child",
    toolCallId: "tool-1",
  });
  return { aborted: true, recovered: true };
}

// The builders have different types for the two wire profiles. Only the
// deliberately shared fixture handler shapes cross this boundary.
let app: any = sdk.agent({ name: "mcp-bridge-fixture" });
let endpoint: McpServerHttpDeclaration;
app = app
  .onNotification("mcp/message", caller.handleNotification)
  .onRequest("initialize", () =>
    version === 1
      ? { protocolVersion: 1, agentCapabilities: { mcpCapabilities: { acp: true } } }
      : { protocolVersion: 2, info: { name: "mcp-bridge-fixture", version: "1" },
          capabilities: { session: { mcp: { acp: {} } } } },
  )
  .onRequest("session/new", async ({ params, client }: any) => {
    assert.equal(params.mcpServers.length, 1);
    assert.equal(params.mcpServers[0].type, "acp");
    endpoint = await bridge.toHttp(client, params.mcpServers[0]);
    return { sessionId: "parent" };
  })
  .onRequest("_interop/mcp-bridge", (params: unknown) => params, ({ client }: any) =>
    exercise(endpoint, async () => {
      await client.notify("session/update", {
        sessionId: "parent",
        update: { sessionUpdate: "subagent_update", sessionId: "child" },
      });
      await client.notify("session/update", {
        sessionId: "child",
        update: version === 1
          ? { sessionUpdate: "tool_call", toolCallId: "tool-1", title: "Fixture" }
          : { sessionUpdate: "tool_call_update", toolCallId: "tool-1", name: "Fixture" },
      });
      await client.request("_interop/mcp-observed", {});
    }),
  );

const conn = app.connect(sdk.ndJsonStream(Writable.toWeb(process.stdout), Readable.toWeb(process.stdin)));
await conn.closed;
