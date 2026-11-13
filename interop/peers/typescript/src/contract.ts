// Versioned SDK apps own validation, framing, dispatch and request IDs.
// Dynamic JSON here is test fixture data, never an alternative RPC transport.
import * as v1 from "@agentclientprotocol/sdk";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { readFile, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { Readable, Writable } from "node:stream";
import { dirname, join } from "node:path";
import { isDeepStrictEqual } from "node:util";
type J = any;
const opts = new Map<string, string>();
for (let i = 2; i < process.argv.length; i += 2)
  opts.set(process.argv[i]!, process.argv[i + 1]!);
const version = Number(opts.get("--version"));
const profile = opts.get("--profile")!;
const sdk: J = version === 2 ? v2 : v1;
const vectors = JSON.parse(await readFile(opts.get("--updates")!, "utf8"));
const reverse = JSON.parse(
  await readFile(join(dirname(opts.get("--updates")!), "reverse.json"), "utf8"),
);
function match(w: J, g: J): void {
  if (w === null || typeof w !== "object") {
    if (!isDeepStrictEqual(w, g))
      throw Error(`expected ${JSON.stringify(w)} got ${JSON.stringify(g)}`);
    return;
  }
  if (Array.isArray(w)) {
    if (!Array.isArray(g) || g.length !== w.length)
      throw Error("array mismatch");
    w.forEach((x, i) => match(x, g[i]));
    return;
  }
  if (g === null || typeof g !== "object" || Array.isArray(g))
    throw Error("expected object");
  for (const k of Object.keys(w)) {
    if (!g || !(k in g)) throw Error(`missing ${k}`);
    match(w[k], g[k]);
  }
}
class Engine {
  sessions: J = {};
  history: J = {};
  pending: J[] = [];
  toolCancelable = false;
  terminalCancelable = false;
  mutations = 0;
  foreground = false;
  background = false;
  requestCancelled = false;
  loggedIn = false;
  config = false;
  reject(): never {
    throw sdk.RequestError.invalidParams("contract fixture rejected request");
  }
  missing(): never {
    if (version === 2) throw new sdk.RequestError(-32002, "Session not found");
    return this.reject();
  }
  handle(method: string, p: J): { result: J; updates: J[] } {
    const done = (result: J = {}, updates: J[] = []) => ({ result, updates });
    if (
      ["no-session", "null-session"].includes(profile) &&
      method.startsWith("session/")
    )
      this.reject();
    if (method === "initialize") {
      const caps: J = {};
      if (profile === "null") caps.systemPrompt = null;
      else if (profile === "append") caps.systemPrompt = {};
      else if (profile === "full")
        caps.systemPrompt = { mode: { override: {} } };
      if (version === 2) {
        if (["no-session", "null-session"].includes(profile))
          return done({
            protocolVersion: 2,
            info: { name: "typescript-contract", version: "1" },
            capabilities: profile === "no-session" ? {} : { session: null },
          });
        caps.delete = {};
        caps.mcp = { stdio: {} };
        return done({
          protocolVersion: 2,
          info: { name: "typescript-contract", version: "1" },
          capabilities: { session: caps },
          authMethods: [
            { type: "agent", methodId: "fixture", name: "Fixture" },
          ],
        });
      }
      caps.list = {};
      caps.resume = {};
      return done({
        protocolVersion: 1,
        agentCapabilities: { loadSession: true, sessionCapabilities: caps },
      });
    }
    if (method === "_interop/flush") {
      const updates = this.pending;
      this.pending = [];
      return done({}, updates);
    }
    if (method === "auth/login") {
      if (p.methodId !== "fixture") this.reject();
      this.loggedIn = true;
      return done();
    }
    if (method === "auth/logout") {
      this.loggedIn = false;
      return done();
    }
    if (method === "session/set_config_option") {
      if (
        p.configId !== "verbose" ||
        p.type !== "boolean" ||
        typeof p.value !== "boolean"
      )
        this.reject();
      if (!this.sessions[p.sessionId]) this.missing();
      this.config = p.value;
      this.mutations++;
      return done({
        configOptions: [
          {
            configId: "verbose",
            name: "Verbose",
            type: "boolean",
            currentValue: this.config,
          },
        ],
      });
    }
    if (method === "_interop/inspect")
      return done({
        mutations: this.mutations,
        sessions: this.sessions,
        foreground: this.foreground,
        background: this.background,
        requestCancelled: this.requestCancelled,
        loggedIn: this.loggedIn,
        config: this.config,
      });
    if (["session/new", "session/load", "session/resume"].includes(method)) {
      if (p.replayFrom && p.replayFrom.type !== "start") this.reject();
      let prompt = "";
      if (p.systemPrompt != null) {
        const sp = p.systemPrompt;
        if (
          !["full", "append"].includes(profile) ||
          !["append", "override"].includes(sp.mode) ||
          (sp.mode === "override" && profile !== "full") ||
          !Array.isArray(sp.content) ||
          !sp.content.length
        )
          this.reject();
        if (
          sp.content.some(
            (b: J) => b.type !== "text" || typeof b.text !== "string",
          )
        )
          this.reject();
        prompt = sp.mode + ":" + sp.content.map((b: J) => b.text).join(" ");
      }
      if (typeof p.cwd !== "string" || !p.cwd) this.reject();
      let id = p.sessionId;
      if (method === "session/new") {
        id = `s${Object.keys(this.sessions).length + 1}`;
        this.sessions[id] = {
          prompt: "default",
          permissions: "ask",
          mcpServers: p.mcpServers ?? [],
          active: true,
        };
      } else if (!this.sessions[id]) this.missing();
      const session = this.sessions[id];
      if (prompt) session.prompt = prompt;
      session.active = true;
      this.mutations++;
      return method === "session/new"
        ? done({
            sessionId: id,
            ...(version === 2
              ? {
                  configOptions: [
                    {
                      configId: "verbose",
                      name: "Verbose",
                      type: "boolean",
                      currentValue: this.config,
                    },
                  ],
                }
              : {}),
          })
        : done(
            {},
            version === 2
              ? [
                  ...(p.replayFrom?.type === "start"
                    ? (this.history[id] ?? [])
                    : []),
                  {
                    background: [],
                    sessionUpdate: "state_update",
                    state: "idle",
                    stopReason: "end_turn",
                  },
                ]
              : [],
          );
    }
    if (method === "session/list")
      return done({
        sessions: Object.keys(this.sessions)
          .sort()
          .map((sessionId) => ({ sessionId, cwd: "/tmp" })),
      });
    if (["session/close", "session/delete"].includes(method)) {
      const s = this.sessions[p.sessionId];
      if (!s) this.missing();
      if (method === "session/delete") delete this.sessions[p.sessionId];
      else {
        s.active = false;
        this.foreground = false;
        this.background = false;
      }
      this.mutations++;
      return done();
    }
    if (method === "session/prompt") {
      if (!this.sessions[p.sessionId]?.active) this.missing();
      if (!p.prompt?.length) this.reject();
      if (p.prompt.some((b: J) => !["text", "resource_link"].includes(b.type)))
        this.reject();
      this.history[p.sessionId] ??= [];
      this.history[p.sessionId].push({
        sessionUpdate: "user_message",
        messageId: p.prompt[0].text === "cancel" ? "m2" : "m1",
        content: p.prompt,
      });
      const outgoing = structuredClone(vectors);
      outgoing[0].content = p.prompt;
      if (p.prompt[0].text === "cancel") {
        this.foreground = true;
        this.background = true;
        return done({ messageId: "m2" }, [
          { sessionUpdate: "user_message", messageId: "m2", content: p.prompt },
          {
            sessionUpdate: "state_update",
            state: "running",
            background: [{ type: "tool_call", toolCallId: "bg" }],
          },
        ]);
      }
      this.toolCancelable = true;
      this.terminalCancelable = true;
      if (p.prompt[0].text === "after") {
        this.pending = outgoing;
        this.pending[0].content = p.prompt;
        return done({ messageId: "m1" });
      }
      return done({ messageId: "m1" }, outgoing);
    }
    if (method === "session/cancel") {
      this.foreground = false;
      return done({}, [
        {
          sessionUpdate: "state_update",
          state: "idle",
          stopReason: "cancelled",
          background: [{ type: "tool_call", toolCallId: "bg" }],
        },
      ]);
    }
    if (method === "tool_call/cancel") {
      if (p.toolCallId !== "t2" || !this.toolCancelable) this.reject();
      this.toolCancelable = false;
      return done({}, [
        {
          sessionUpdate: "tool_call_update",
          toolCallId: "t2",
          status: "completed",
          capabilities: null,
        },
      ]);
    }
    if (method === "terminal/cancel") {
      if (p.terminalId !== "term2" || !this.terminalCancelable) this.reject();
      this.terminalCancelable = false;
      return done({}, [
        {
          sessionUpdate: "terminal_update",
          terminalId: "term2",
          exitStatus: { exitCode: 130 },
          capabilities: null,
        },
      ]);
    }
    throw sdk.RequestError.methodNotFound(method);
  }
}
const methods = [
  "initialize",
  "session/new",
  "session/resume",
  "session/list",
  "_interop/inspect",
  "_interop/flush",
  ...(version === 1
    ? ["session/load"]
    : [
        "session/close",
        "session/delete",
        "session/prompt",
        "tool_call/cancel",
        "terminal/cancel",
        "auth/login",
        "auth/logout",
        "session/set_config_option",
      ]),
];
async function main() {
  if (opts.get("--role") === "agent") {
    const e = new Engine();
    let app: J = sdk.agent({ name: "typescript-contract" });
    for (const method of methods) {
      const handler = async (ctx: J) => {
        if (
          version === 2 &&
          method === "session/prompt" &&
          ctx.params.prompt[0]?.text === "request-cancel"
        ) {
          await ctx.client.notify("session/update", {
            sessionId: ctx.params.sessionId,
            update: {
              sessionUpdate: "notice",
              severity: "info",
              title: "request-ready",
            },
          });
          await new Promise<void>((resolve) => {
            if (ctx.signal.aborted) resolve();
            else
              ctx.signal.addEventListener("abort", () => resolve(), {
                once: true,
              });
          });
          e.requestCancelled = true;
          await ctx.client.notify("session/update", {
            sessionId: ctx.params.sessionId,
            update: {
              sessionUpdate: "notice",
              severity: "info",
              title: "request-cancelled",
            },
          });
          throw new sdk.RequestError(-32800, "Request cancelled");
        }
        const r = e.handle(method, ctx.params);
        for (const update of r.updates) {
          await ctx.client.notify("session/update", {
            sessionId: ctx.params.sessionId,
            update,
          });
          if (update.state === "requires_action") {
            for (const r of reverse) {
              if (r.notify) await ctx.client.notify(r.method, r.params);
              else
                match(r.response, await ctx.client.request(r.method, r.params));
            }
          }
        }
        return r.result;
      };
      app = method.startsWith("_")
        ? app.onRequest(method, (p: J) => p, handler)
        : app.onRequest(method, handler);
    }
    if (version === 2)
      app = app.onNotification("session/cancel", async (ctx: J) => {
        for (const update of e.handle("session/cancel", ctx.params).updates)
          await ctx.client.notify("session/update", {
            sessionId: ctx.params.sessionId,
            update,
          });
      });
    const connection = app.connect(
      sdk.ndJsonStream(
        Writable.toWeb(process.stdout),
        Readable.toWeb(process.stdin),
      ),
    );
    await connection.closed;
    return;
  }
  const c = JSON.parse(await readFile(opts.get("--case")!, "utf8"));
  const child = spawn(
    opts.get("--agent")!,
    [
      "-version",
      String(version),
      "-profile",
      profile,
      "-updates",
      opts.get("--updates")!,
    ],
    { stdio: ["pipe", "pipe", "pipe"] },
  );
  child.stderr.on("data", (x) => process.stderr.write(x));
  const updates: J[] = [];
  const trace: string[] = [];
  const reverseTrace: string[] = [];
  let offset = 0;
  let app: J = sdk
    .client({ name: "typescript-contract" })
    .onNotification("session/update", (ctx: J) => {
      updates.push(ctx.params.update);
    });
  for (const method of new Set(
    reverse.filter((r: J) => !r.notify).map((r: J) => r.method),
  )) {
    app = app.onRequest(method, (ctx: J) => {
      for (const r of reverse) {
        if (r.method !== method) continue;
        try {
          match(r.params, ctx.params);
        } catch {
          continue;
        }
        reverseTrace.push(String(method));
        return r.response;
      }
      throw sdk.RequestError.invalidParams("unexpected reverse params");
    });
  }
  app = app.onNotification("elicitation/complete", (ctx: J) => {
    match(reverse.find((r: J) => r.notify).params, ctx.params);
    reverseTrace.push("elicitation/complete");
  });
  try {
    await app.connectWith(
      sdk.ndJsonStream(
        Writable.toWeb(child.stdin),
        Readable.toWeb(child.stdout),
      ),
      async (agent: J) => {
        for (const [i, s] of c.steps.entries()) {
          try {
            if (s.cancel) {
              const controller = new AbortController();
              const pending = agent.request(s.method, s.params, {
                cancellationSignal: controller.signal,
              });
              const end = Date.now() + 10000;
              while (updates.length <= offset) {
                if (Date.now() > end) throw Error("missing cancellation ready");
                await new Promise((r) => setTimeout(r, 2));
              }
              controller.abort();
              await pending;
            } else if (s.notify) await agent.notify(s.method, s.params);
            else {
              const r = await agent.request(s.method, s.params);
              if (s.error) throw Error(`expected error ${s.error}`);
              match(s.expect, r);
            }
          } catch (e) {
            if (
              !s.error ||
              !(e instanceof sdk.RequestError) ||
              (e as J).code !== s.error
            )
              throw Error(`step ${i} ${s.method}: ${e}`);
          }
          for (const [j, w] of (s.updates ?? []).entries()) {
            const end = Date.now() + 10000;
            while (updates.length <= offset + j) {
              if (Date.now() > end) throw Error(`missing update ${i}/${j}`);
              await new Promise((r) => setTimeout(r, 2));
            }
            match(w, updates[offset + j]);
          }
          offset += (s.updates ?? []).length;
          trace.push(`${i}:${s.method}:ok`);
        }
      },
    );
  } finally {
    child.stdin.end();
    child.kill();
  }
  await writeFile(
    opts.get("--result")!,
    JSON.stringify({ trace, reverse: reverseTrace }),
  );
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
