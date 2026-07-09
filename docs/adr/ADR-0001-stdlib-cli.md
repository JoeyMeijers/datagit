# ADR-0001: Standard-library CLI instead of Cobra

## Status

Accepted

## Context

The project brief names Cobra as the CLI framework. Cobra (all versions) transitively requires modules served from vanity import domains (`gopkg.in`, `go.yaml.in`) that were unreachable in the initial development environment, which can only fetch modules hosted directly on github.com. datagit's CLI needs are currently modest: eight subcommands, few flags.

## Decision

Implement the CLI with `flag` from the standard library plus a small command table (`internal/cli`). Each command is a self-contained `Command` value; dispatch is a map lookup.

## Consequences

### Positive

- Zero CLI dependencies; smaller binary; nothing to audit.
- Commands take an `io.Writer` and are directly unit-testable.
- The command-table shape mirrors Cobra's model, keeping a later migration mechanical.

### Negative

- No automatic shell completion, nested subcommands, or generated docs.
- Flag parsing conveniences (persistent flags, aliases) must be hand-rolled if needed.

## Alternatives

- Cobra: blocked by unreachable transitive dependencies; revisit when the environment allows and command complexity justifies it.
- urfave/cli: same class of benefits/costs as Cobra; unnecessary at current scale.
