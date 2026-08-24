#!/usr/bin/env node

import * as acp from "@agentclientprotocol/sdk";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { Readable, Writable } from "node:stream";

type Role = "agent" | "client";
type Scenario = "core" | "session-cancel" | "request-cancel";

interface Options {
  role: Role;
  scenario: Scenario;
  resultPath?: string;
  agentPath?: string;
}

interface Observation {
  peer: "typescript";
  role: Role;
  scenario: Scenario;
  protocolVersion?: number;
  capabilitiesUnsupported?: boolean;
  sessionId?: string;
  stopReason?: string;
  errorCode?: number;
  requestCancelObserved?: boolean;
  events: string[];
}

function parseOptions(argv: string[]): Options {
  const values = new Map<string, string>();
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key?.startsWith("--") || value === undefined) {
      throw new Error(`invalid argument sequence near ${key ?? "<end>"}`);
    }
    values.set(key.slice(2), value);
  }

  const role = values.get("role");
  const scenario = values.get("scenario");
  if (role !== "agent" && role !== "client") {
    throw new Error("--role must be agent or client");
  }
  if (
    scenario !== "core" &&
    scenario !== "session-cancel" &&
    scenario !== "request-cancel"
  ) {
    throw new Error("--scenario must be core, session-cancel, or request-cancel");
  }

  return {
    role,
    scenario,
    resultPath: values.get("result"),
    agentPath: values.get("agent"),
  };
}

async function writeObservation(path: string, value: Observation): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  const temporaryPath = `${path}.tmp-${process.pid}`;
  await writeFile(temporaryPath, `${JSON.stringify(value)}\n`, "utf8");
  await rename(temporaryPath, path);
}

function textFromPrompt(prompt: acp.ContentBlock[]): string {
  return prompt
    .map((block) => (block.type === "text" ? block.text : ""))
    .filter((text) => text !== "")
    .join(" ");
}

function textFromUpdate(notification: acp.SessionNotification): string | undefined {
  const update = notification.update;
  if (
    update.sessionUpdate === "agent_message_chunk" &&
    update.content.type === "text"
  ) {
    return update.content.text;
  }
  return undefined;
}

async function waitForAbort(signal: AbortSignal): Promise<void> {
  if (signal.aborted) {
    return;
  }
  await new Promise<void>((resolve) => {
    signal.addEventListener("abort", () => resolve(), { once: true });
  });
}

async function waitForEvent(events: string[], expected: string): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (!events.includes(expected)) {
    if (Date.now() >= deadline) {
      throw new Error(`timed out waiting for ${expected}; saw ${events.join(", ")}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

async function runAgent(options: Options): Promise<void> {
  const sessions = new Set<string>();
  const sessionCancels = new Map<string, AbortController>();
  let nextSession = 0;

  const app = acp
    .agent({ name: "acp-go-sdk-typescript-interop-agent" })
    .onRequest(acp.methods.agent.initialize, (ctx) => ({
      protocolVersion: ctx.params.protocolVersion,
      agentCapabilities: {},
      authMethods: [],
      agentInfo: {
        name: "typescript-interop-agent",
        version: "1.4.0",
      },
    }))
    .onRequest(acp.methods.agent.session.new, () => {
      nextSession += 1;
      const sessionId = `typescript-session-${nextSession}`;
      sessions.add(sessionId);
      return { sessionId };
    })
    .onRequest(acp.methods.agent.session.prompt, async (ctx) => {
      const { sessionId } = ctx.params;
      if (!sessions.has(sessionId)) {
        throw acp.RequestError.invalidParams({ sessionId });
      }

      const scenario = textFromPrompt(ctx.params.prompt);
      if (scenario === "core") {
        await ctx.client.notify(acp.methods.client.session.update, {
          sessionId,
          update: {
            sessionUpdate: "agent_message_chunk",
            content: { type: "text", text: "core-1" },
          },
        });
        await ctx.client.notify(acp.methods.client.session.update, {
          sessionId,
          update: {
            sessionUpdate: "agent_message_chunk",
            content: { type: "text", text: "core-2" },
          },
        });
        const permission = await ctx.client.request(
          acp.methods.client.session.requestPermission,
          {
            sessionId,
            toolCall: {
              toolCallId: "interop-tool",
              title: "Interop permission",
              kind: "execute",
              status: "pending",
            },
            options: [
              { optionId: "allow", name: "Allow", kind: "allow_once" },
              { optionId: "reject", name: "Reject", kind: "reject_once" },
            ],
          },
        );
        if (
          permission.outcome.outcome !== "selected" ||
          permission.outcome.optionId !== "allow"
        ) {
          throw new Error(`unexpected permission outcome ${JSON.stringify(permission)}`);
        }
        await ctx.client.notify(acp.methods.client.session.update, {
          sessionId,
          update: {
            sessionUpdate: "agent_message_chunk",
            content: { type: "text", text: "core-3" },
          },
        });
        return { stopReason: "end_turn" };
      }

      if (scenario === "session-cancel") {
        const controller = new AbortController();
        sessionCancels.set(sessionId, controller);
        await ctx.client.notify(acp.methods.client.session.update, {
          sessionId,
          update: {
            sessionUpdate: "agent_message_chunk",
            content: { type: "text", text: "session-cancel-ready" },
          },
        });
        await waitForAbort(controller.signal);
        sessionCancels.delete(sessionId);
        return { stopReason: "cancelled" };
      }

      if (scenario === "request-cancel") {
        await ctx.client.notify(acp.methods.client.session.update, {
          sessionId,
          update: {
            sessionUpdate: "agent_message_chunk",
            content: { type: "text", text: "request-cancel-ready" },
          },
        });
        await waitForAbort(ctx.signal);
        if (options.resultPath) {
          await writeObservation(options.resultPath, {
            peer: "typescript",
            role: "agent",
            scenario: "request-cancel",
            requestCancelObserved: true,
            events: ["request-cancel-observed"],
          });
        }
        const error = new Error("request cancelled");
        error.name = "AbortError";
        throw error;
      }

      throw acp.RequestError.invalidParams({ scenario });
    })
    .onNotification(acp.methods.agent.session.cancel, (ctx) => {
      sessionCancels.get(ctx.params.sessionId)?.abort();
    });

  const output = Writable.toWeb(process.stdout);
  const input = Readable.toWeb(process.stdin) as ReadableStream<Uint8Array>;
  const connection = app.connect(acp.ndJsonStream(output, input));
  await connection.closed;
}

function spawnAgent(path: string): ChildProcessWithoutNullStreams {
  const child = spawn(path, [], { stdio: ["pipe", "pipe", "pipe"] });
  child.stderr.on("data", (chunk: Buffer) => process.stderr.write(chunk));
  return child;
}

async function stopAgent(child: ChildProcessWithoutNullStreams): Promise<void> {
  child.stdin.end();
  if (await waitForExit(child, 2_000)) {
    return;
  }
  child.kill();
  if (!(await waitForExit(child, 2_000))) {
    throw new Error("Go interop agent did not exit after SIGTERM");
  }
}

function waitForExit(
  child: ChildProcessWithoutNullStreams,
  timeoutMilliseconds: number,
): Promise<boolean> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return Promise.resolve(true);
  }
  return new Promise((resolve) => {
    const onExit = (): void => finish(true);
    const timer = setTimeout(() => finish(false), timeoutMilliseconds);
    const finish = (exited: boolean): void => {
      clearTimeout(timer);
      child.off("exit", onExit);
      resolve(exited);
    };
    child.once("exit", onExit);
  });
}

async function runClient(options: Options): Promise<void> {
  if (!options.agentPath || !options.resultPath) {
    throw new Error("client role requires --agent and --result");
  }

  const child = spawnAgent(options.agentPath);
  await new Promise<void>((resolve, reject) => {
    child.once("spawn", () => resolve());
    child.once("error", reject);
  });

  const events: string[] = [];
  const app = acp
    .client({ name: "acp-go-sdk-typescript-interop-client" })
    .onRequest(acp.methods.client.session.requestPermission, (ctx) => {
      const selected = ctx.params.options.find((option) => option.optionId === "allow");
      if (!selected) {
        throw new Error("Go agent did not offer the deterministic allow option");
      }
      events.push("permission:allow");
      return {
        outcome: { outcome: "selected", optionId: selected.optionId },
      };
    })
    .onNotification(acp.methods.client.session.update, (ctx) => {
      const text = textFromUpdate(ctx.params);
      if (text !== undefined) {
        events.push(`update:${text}`);
      }
    });

  const observation: Observation = {
    peer: "typescript",
    role: "client",
    scenario: options.scenario,
    events,
  };

  const output = Writable.toWeb(child.stdin);
  const input = Readable.toWeb(child.stdout) as ReadableStream<Uint8Array>;
  try {
    await app.connectWith(acp.ndJsonStream(output, input), async (agent) => {
      const initialized = await agent.request(acp.methods.agent.initialize, {
        protocolVersion: acp.PROTOCOL_VERSION,
        clientCapabilities: {},
        clientInfo: {
          name: "typescript-interop-client",
          version: "1.4.0",
        },
      });
      observation.protocolVersion = initialized.protocolVersion;
      const capabilities = initialized.agentCapabilities;
      observation.capabilitiesUnsupported =
        capabilities === undefined ||
        (!capabilities.loadSession &&
          !capabilities.promptCapabilities?.audio &&
          !capabilities.promptCapabilities?.embeddedContext &&
          !capabilities.promptCapabilities?.image);

      const session = await agent.request(acp.methods.agent.session.new, {
        cwd: process.cwd(),
        mcpServers: [],
      });
      observation.sessionId = session.sessionId;
      const request: acp.PromptRequest = {
        sessionId: session.sessionId,
        prompt: [{ type: "text" as const, text: options.scenario }],
      };

      if (options.scenario === "core") {
        const response = await agent.request(acp.methods.agent.session.prompt, request);
        observation.stopReason = response.stopReason;
        events.push(`response:${response.stopReason}`);
        return;
      }

      if (options.scenario === "session-cancel") {
        const response = agent.request(acp.methods.agent.session.prompt, request);
        await waitForEvent(events, "update:session-cancel-ready");
        await agent.notify(acp.methods.agent.session.cancel, {
          sessionId: session.sessionId,
        });
        const completed = await response;
        observation.stopReason = completed.stopReason;
        events.push(`response:${completed.stopReason}`);
        return;
      }

      const controller = new AbortController();
      const response = agent.request(acp.methods.agent.session.prompt, request, {
        cancellationSignal: controller.signal,
      });
      await waitForEvent(events, "update:request-cancel-ready");
      controller.abort();
      try {
        await response;
        throw new Error("request cancellation unexpectedly returned a normal response");
      } catch (error) {
        if (!(error instanceof acp.RequestError)) {
          throw error;
        }
        observation.errorCode = error.code;
        events.push(`error:${error.code}`);
      }
    });
  } finally {
    await stopAgent(child);
  }

  await writeObservation(options.resultPath, observation);
}

async function main(): Promise<void> {
  const options = parseOptions(process.argv.slice(2));
  if (options.role === "agent") {
    await runAgent(options);
  } else {
    await runClient(options);
  }
}

main().catch((error: unknown) => {
  console.error(error);
  process.exitCode = 1;
});
