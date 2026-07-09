# AGENTS.md

# Universal AI Engineering Standards

This file defines mandatory behavior for all AI coding agents working in this repository
(Claude Code, Cursor, GitHub Copilot, Codex, Gemini CLI, Aider, Roo Code, etc.).

The objective is to maximize:

- Correctness
- Maintainability
- Reliability
- Testability
- Security
- Performance
- Documentation quality
- Long-term sustainability

---

# Core Philosophy

Always optimize for the engineer who will maintain this code five years from now.

Prefer:

- Simplicity
- Explicitness
- Small components
- Composition over inheritance
- Pure functions where practical
- Low coupling
- High cohesion

Avoid:

- Clever code
- Hidden side effects
- Premature optimization
- Global mutable state
- Large functions
- Large classes
- Duplicate logic

---

# Before Writing Code

Always:

1. Understand the existing architecture.
2. Search for similar implementations.
3. Reuse existing abstractions.
4. Minimize the scope of changes.
5. Consider backwards compatibility.

Never rewrite working code without a clear benefit.

---

# Design Principles

Follow:

- SOLID
- DRY
- KISS
- YAGNI
- Separation of Concerns
- Principle of Least Astonishment

When principles conflict:

Correctness > Simplicity > Performance > Cleverness

---

# Unit Testing (Mandatory)

Every production code change MUST include automated tests.

Required:

- New feature -> new tests
- Bug fix -> regression test
- Refactor -> preserve or improve coverage

Tests must:

- Be deterministic
- Run offline
- Not depend on wall clock time
- Not depend on external APIs
- Not depend on random values unless seeded

Mock:

- HTTP
- Databases
- Message queues
- File systems when appropriate

Prefer:

1. Unit tests
2. Integration tests
3. End-to-end tests

Minimum expectations:

- Business logic: >95% coverage
- Utility functions: 100% coverage where practical
- Critical algorithms: exhaustive edge case testing

Required edge cases:

- Empty input
- Null / None
- Invalid values
- Boundary conditions
- Large inputs
- Concurrent access
- Failure scenarios

Never remove tests unless functionality itself is removed.

---

# Documentation (Mandatory)

Documentation is part of the definition of done.

Update whenever behavior changes:

- README.md
- API docs
- Configuration docs
- Usage examples
- Migration guides
- Developer setup

Public functions should contain useful documentation.

Comments should explain WHY, not WHAT.

---

# ADR Policy (Mandatory)

Create or update an ADR whenever changing:

- Architecture
- Framework selection
- Database design
- API contracts
- Infrastructure
- Security model
- Caching strategy
- Concurrency model
- Performance trade-offs
- Third-party dependencies

Location:

/docs/adr/

Format:

# ADR-XXXX: Title

## Status

Proposed | Accepted | Deprecated | Superseded

## Context

## Decision

## Consequences

### Positive

### Negative

## Alternatives

Use sequential numbering.

---

# Code Reviews

Before considering work complete:

- [ ] Code compiles
- [ ] Linter passes
- [ ] Formatter passes
- [ ] Static analysis passes
- [ ] Tests pass
- [ ] New tests added
- [ ] Documentation updated
- [ ] ADR updated if needed
- [ ] No dead code
- [ ] No debug prints
- [ ] No commented-out code
- [ ] No secrets committed

---

# Dependency Policy

Before adding a dependency ask:

1. Can the standard library solve this?
2. Is the dependency actively maintained?
3. Is it widely adopted?
4. Does it introduce security risks?
5. Does it increase build complexity?

Significant dependencies require an ADR.

Remove unused dependencies.

---

# Security

Always:

- Validate inputs
- Escape outputs
- Use parameterized queries
- Follow least privilege
- Protect secrets
- Fail securely

Never:

- Hardcode credentials
- Log secrets
- Disable TLS verification
- Ignore security warnings
- Suppress exceptions silently

Check for:

- Injection attacks
- Path traversal
- Race conditions
- Integer overflow
- Resource exhaustion

---

# Performance

Measure before optimizing.

Optimize only after identifying bottlenecks.

Consider:

- Time complexity
- Memory usage
- I/O
- Allocation behavior
- Network round trips

Document major optimizations in ADRs.

---

# Error Handling

Never swallow exceptions.

Errors should:

- Be explicit
- Preserve context
- Contain actionable messages

Prefer returning structured errors.

---

# Logging

Logs should help diagnose production issues.

Never log:

- Passwords
- Tokens
- API keys
- Personal data
- Secrets

Use appropriate log levels.

---

# Git

Prefer small atomic commits.

Use Conventional Commits:

- feat:
- fix:
- docs:
- test:
- refactor:
- perf:
- build:
- ci:
- chore:

---

# CI/CD Expectations

Every pull request should automatically run:

- Formatting
- Linting
- Static analysis
- Unit tests
- Integration tests
- Security scan
- Dependency vulnerability scan

A failing pipeline blocks merging.

---

# Language Guidelines

## Python

- Type hints required
- Prefer pathlib
- Use dataclasses where appropriate
- Avoid mutable default arguments

## Go

- Always check errors
- Keep interfaces small
- Favor composition

## Rust

- Prefer Result over panic
- Avoid unwrap() in production
- Document unsafe blocks

## C#

- Nullable reference types enabled
- Use async appropriately
- Prefer dependency injection

## JavaScript / TypeScript

- TypeScript preferred
- Strict mode enabled
- Avoid any

---

# AI Agent Workflow

For every task:

1. Understand the problem.
2. Inspect existing code.
3. Design minimal solution.
4. Implement.
5. Add tests.
6. Update documentation.
7. Create/update ADR if required.
8. Run quality checks mentally.
9. Verify consistency.

If requirements are ambiguous:

STOP and ask for clarification.

Never invent APIs, files, or behavior that do not exist.

Never claim code was executed if it was not.

Never remove documentation, tests, or ADRs simply because they appear outdated.

Update them instead.

---

# Definition of Done

A task is only complete when:

✓ Code implemented

✓ Tests added

✓ Tests passing

✓ Documentation updated

✓ ADR updated if applicable

✓ No obvious technical debt introduced

✓ Code is maintainable by another engineer

If any item is missing, the work is NOT complete.
