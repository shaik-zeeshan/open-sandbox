# Effect-TS Best Practices

## Design principles

- Keep core logic pure; isolate IO in a thin shell.
- Model errors explicitly with tagged unions; avoid exceptions.
- Prefer immutable data and total functions.

## Composition

- Use `pipe` plus `Effect.flatMap` or `Effect.map`, or `Effect.gen` for sequential flows.
- Interop with `Promise` only at boundaries via `Effect.try` or `Effect.tryPromise`.
- Use `Match.exhaustive` for union handling; avoid `switch` in domain logic.

## Dependency injection

- Define services with `Context.Tag` and small interfaces.
- Provide live layers at runtime; provide test layers in unit tests.
- Keep service interfaces free of concrete implementations and globals.

## Boundary validation

- Accept `unknown` at the boundary only.
- Decode with `@effect/schema` and pass validated types into core.
- Fail fast on invalid input; keep validation errors typed.

## Resource safety

- Use `Effect.acquireRelease` for resources such as connections, files, or locks.
- Use `Effect.scoped` to control lifetimes and ensure finalizers run.

## Platform usage

- Use `@effect/platform` services instead of host APIs:
  - `HttpClient` or `HttpServer` for HTTP
  - `FileSystem` or `Path` for files and paths
  - `Command` or `Terminal` for CLI and processes
  - `KeyValueStore` for local storage-like needs

## Runtime and entrypoints

- Use `Effect.runMain` or platform runtime helpers for application entry.
- Use `Logger` or `PlatformLogger` for structured logging.

## Testing

- Write tests as Effects and provide test layers or mocks.
- Use `TestClock` and `Ref` for deterministic time and state.
- Use property-based tests for invariants when appropriate.

## Minimal example

```ts
import { Context, Effect, Layer, pipe } from "effect"

class Clock extends Context.Tag("Clock")<Clock, {
  readonly nowMillis: Effect.Effect<number, never>
}>() {}

const ClockLive = Layer.succeed(Clock, {
  nowMillis: Effect.sync(() => Date.now())
})

const program = pipe(
  Clock,
  Effect.flatMap((clock) => clock.nowMillis),
  Effect.map((ms) => ({ now: ms }))
)

const main = Effect.provide(program, ClockLive)
```
