---
name: effect-ts-guide
description: Teach and apply Effect-TS practices (effect, @effect/schema, @effect/platform) for architecture, typed errors, Layers, dependency injection, resource safety, testing, and migration from async-await or Promise code.
compatibility: opencode
---

# Effect TS Guide

Use this skill to teach Effect-TS fundamentals and best practices, then apply them to user code and architecture questions.

## Teaching workflow

1. Clarify context: runtime (node, bun, browser), goal (new app, refactor, review), and constraints.
2. Separate core vs shell: identify pure domain logic vs effects and boundaries.
3. Model errors and dependencies: define tagged error types and `Context.Tag` service interfaces.
4. Compose with `Effect`: use `pipe` and `Effect.gen`, typed errors, and `Layer` provisioning.
5. Validate inputs at boundaries with `@effect/schema` before entering core.
6. Explain resource safety: `acquireRelease`, scoped lifetimes, and clean finalizers.
7. Provide minimal, runnable examples tailored to the user context.
8. If the user asks for version-specific or latest details, verify with official docs before answering.

## Core practices

- Use `Effect` for side effects; keep core functions pure and total.
- Avoid `async`/`await`, raw `Promise` chains, and `try`/`catch` in application logic.
- Use `Context.Tag` and `Layer` for dependency injection and testability.
- Use tagged error unions and `Match.exhaustive` for total handling.
- Decode `unknown` at the boundary with `@effect/schema`; do not leak `unknown` into core.
- Use `Effect.acquireRelease` and `Effect.scoped` for resource safety.
- Prefer `@effect/platform` services over direct host APIs for fetch, fs, child processes, and similar boundaries.

## References

- Read `references/best-practices.md` for the extended checklist and examples.
- Read `references/platform-map.md` when comparing `@effect/platform` to Node, Bun, or browser APIs.
