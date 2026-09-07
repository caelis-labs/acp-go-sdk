# Upstream maintenance lock

`lock.json` is the machine-readable contract for which official ACP artifacts
this repository currently tracks:

- stable ACP v1 JSON Schema tags (`schema-v1*`);
- draft ACP v2 schema tags (`schema-v2*`);
- official TypeScript SDK releases;
- official Rust SDK releases.

Exact schema asset hashes remain in `schema/lock.json`. Exact interop package
identities remain in `interop/versions.json`. `make verify-upstream` requires
those files to agree with this lock.

A scheduled GitHub Action runs `go run ./cmd/upstreamdrift -remote` and opens
or updates an `upstream-drift` issue when GitHub reports a newer official tag
or release than the pin. Local CI does not query GitHub; it only checks that
the committed locks are internally consistent.

Policy:

- Stable schema: lock, regenerate, compatibility-review, and interop within
  one week of a new `schema-v1*` release.
- Official SDKs: bump the interop pin and rerun the four-direction matrix on
  every formal TypeScript or Rust SDK release, even when the wire schema is
  unchanged.
- Draft v2: track tagged `schema-v2*` snapshots only. Do not chase every
  commit on upstream `main`, and do not merge v2 types into the stable root
  package.
