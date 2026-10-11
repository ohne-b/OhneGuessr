# Agent instructions

This is the canonical agent entry point. Read [CONTRIBUTING.md](CONTRIBUTING.md)
before planning, editing, testing or reviewing. It owns contributor workflow,
validation, PR requirements and release boundaries for people and agents.

Read the relevant sections of [Architecture and test ownership](docs/architecture.md)
before changing code. Preserve its existing ownership, compatibility and state
contracts; keep technical details there rather than duplicating them here.

Use one implementation agent when requested. Independent review uses a separate
read-only reviewer; pass them the goal, acceptance criteria, final revision and
actual validation evidence, together with the contributor instructions.

Keep this entry point short. Do not create alternate instruction files or planning
documents. Final handoffs state what changed, what was tested and independently
reviewed, remaining limitations, the PR link, and what was committed, merged,
released or deployed.
