Mission
- Build a product-neutral, schema-generated, concurrency-safe ACP Go SDK.
- Stable root package implements stable ACP v1 only.
- Official versioned ACP JSON Schema releases are the sole wire source of truth.

MUST
- Pin exact upstream schema tag, commit and SHA256.
- Generate all stable wire types deterministically.
- Support both string and number JSON-RPC request IDs without precision loss.
- Support bidirectional concurrent requests.
- Preserve notification order per connection.
- Implement $/cancel_request and JSON-RPC -32800 cancellation.
- Bound frame size, pending requests, handler concurrency and write queues.
- Provide idempotent Close and deterministic Wait semantics.
- Use context.Context for every blocking operation.
- Treat omitted capabilities as unsupported.
- Preserve _meta and unknown forward-compatible variants where allowed.
- Expose JSON-RPC code/message/data through errors.As.
- Provide stable stdio NDJSON transport.
- Drain child stderr and never write logs to protocol stdout.
- Run go test -race ./... in CI.
- Provide fuzz tests for JSON decoding, request IDs, unions and framing.
- Prove bidirectional interoperability with official TypeScript and Rust SDKs.
- Keep stable, experimental/v1 and experimental/v2 generated surfaces isolated.
- Generate experimental/v2 from the pinned schema-v2* tag only.
- Preserve license history, NOTICE and upstream attribution.

MUST NOT
- Import github.com/caelis-labs/caelis.
- Implement Agent Runtime, persistence, authorization, replay or Surface projection.
- Move Caelis semantic/projector/eventstream/taskstream into the SDK.
- Add Caelis private extensions to the stable root package.
- Add custom fields to ACP-defined root objects; extensions use _meta.
- Treat _meta as identity, authorization, ordering or durable ownership.
- Silently convert marshal errors to null.
- Hand-edit generated protocol files.
- Generate release code from upstream main.
- Merge unstable schema definitions into the stable API.
- Use unbounded goroutines, channels, queues or message reads.
- Spawn commands through an implicit shell.
- Provide unrestricted default filesystem or terminal handlers.
- Log prompts, file contents, tool output, environment variables or secrets by default.
- Publish HTTP/SSE or WebSocket draft behavior as a stable contract.
- Publish a production-ready tag without cross-SDK conformance evidence.

Validation
- gofmt and git diff --check
- go vet ./...
- static analysis
- go test ./...
- go test -race ./...
- fuzz corpus replay
- generator reproducibility and git diff --exit-code
- schema checksum verification
- upstream lock consistency
- public API diff
- example builds
- TypeScript/Rust interop matrix
- fresh consumer module smoke test
