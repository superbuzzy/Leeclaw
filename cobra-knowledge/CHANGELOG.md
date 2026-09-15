# Changelog

## v0.10.1

- Added OpenClaw-profile-first workspace and downstream account mapping.
- Added the OpenViking-backed Memory adapter and governed promotion closure.
- Persisted the 18790 sandbox route and Chromium/CJK browser runtime.
- Added unified verification and destructive container-recreate regression.

## v0.10.0

- Added controlled Experience Promotion with Candidate / Inspection / Review / Publication / Rollback contracts.
- Added personal/workspace candidate scope and explicit admin-reviewed promotion into shared organizational assets.
- Added storage-neutral Promotion Store with single-node FSStore and optimistic version checks.
- Added WeKnora Knowledge Promotion: evidence revalidation, KB-scope check, duplicate/conflict gate, formal publication and rollback.
- Added OpenViking Skill Promotion: Session/Experience traceability, strict Skill validation, behavior diff, Eval gate, revision-race check, shared Skill add/update/rollback.
- Added independent internal Promotion API protected by `LEECLAW_PROMOTION_TOKEN`.
- Added independent OpenClaw `leeclaw-promotion` server-side plugin; Agent Tool Surface remains unable to publish or rollback formal assets.
- Replaced active v0.9 config/compatibility/verification files with v0.10 equivalents and aligned active package/plugin metadata.
- Kept formal Knowledge in WeKnora, formal Skill in OpenViking, and all three upstream source trees unmodified.

## v0.9.0

- Added Planner v2 with required/supporting sources, freshness and ontology-federation blocking issues.
- Added request-scoped multi-KB ontology federation and visible concept-sense conflicts.
- Added runtime Entity Retriever over WeKnora Neo4j and transport-neutral Business Retriever contracts.
- Expanded Evidence Contract v2 and added required-source Completeness Gate.
- Preserved the rule that RAG fallback cannot replace missing live/structured facts.

## v0.8.0

- Connected Dynamic Skill Runtime and Knowledge Runtime to OpenClaw Agent turns.
- Added server-side KB scope/evidence revalidation and session-pinned Workspace binding.
- Enforced Skill allowed-tools as a narrowing intersection with OpenClaw Tool Authority.

## v0.7.0 and earlier

Historical implementation details are preserved in `RELEASE-v0.2.md` through `RELEASE-v0.9.md` and Git history. Active behavior is defined by the current v0.10 code, configuration and verification gates.
