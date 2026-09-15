# LeeClaw v0.10.1 Release

v0.10.1 is the stabilization patch for the v0.10 architecture. It keeps all
three upstream submodules unmodified and completes the integration through
LeeClaw adapters, deployment configuration, and verification scripts.

## Included

- OpenClaw durable profiles are the only human identity source and are mapped
  server-side to LeeClaw workspaces, OpenViking accounts, and WeKnora scopes.
- `LeeClawMemoryService` backs the OpenClaw Memory surface with OpenViking and
  prevents a second automatically growing long-term-memory store.
- Experience promotion performs source verification, duplicate/conflict
  checks, review, pre-publish recheck, publication reference recording, and
  rollback handling before WeKnora is changed.
- Gateway port 18789 and MCP Apps sandbox port 18790 are both exposed through
  the persistent deployment path and have layer-specific diagnostics.
- Chromium, runtime libraries, Noto CJK/emoji fonts, fontconfig, and browser
  verification are built into the LeeClaw OpenClaw image.
- The unified v0.10 verifier includes account, backend mapping, memory,
  promotion, sandbox, browser, persistence, and upstream-clean gates.

## Upgrade invariant

Recreating the application containers must not require manual package
installation, font copying, port forwarding, browser binary repair, or
workspace/backend remapping.
