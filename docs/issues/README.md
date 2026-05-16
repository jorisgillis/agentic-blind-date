# Agentic Blind Date - Implementation Issues

This directory contains issue descriptions for tracking implementation work. Each file represents a single issue that can be created in GitHub.

## Issue List

### 🔴 High Priority

| # | Issue | Priority | Labels | Type |
|---|-------|----------|--------|------|
| 001 | [Update CONTEXT.md: Add Non-GitHub User section](001-update-context-non-github-users.md) | High | enhancement, documentation, domain | Documentation |
| 002 | [Update CONTEXT.md: Standardize on creating_persona pipeline step](002-update-context-pipeline-steps.md) | High | enhancement, documentation, domain | Documentation |
| 003 | [Implement creating_persona pipeline step in RunFinalSetup](003-implement-creating-persona-step.md) | High | enhancement, backend | Feature |
| 005 | [Add backend validation for multi-select answers](005-add-multiselect-validation.md) | High | enhancement, backend, validation | Feature |
| 006 | [Return 400 Bad Request for invalid multi-select submissions](006-return-400-for-invalid-multiselect.md) | High | enhancement, backend, validation | Feature |
| 007 | [Add tests for all existing untested code to reach 100% coverage](007-add-tests-for-100-percent-coverage.md) | High | enhancement, testing | Testing |

### 🟡 Medium Priority

| # | Issue | Priority | Labels | Type |
|---|-------|----------|--------|------|
| 004 | [Update templates: Change creating_persona display message](004-update-templates-creating-persona-message.md) | Medium | enhancement, frontend | Feature |
| 008 | [Create ADR 0006: Fallback Questions for LLM Failures](008-create-adr-0006-llm-fallbacks.md) | High | enhancement, documentation, architecture | Documentation |
| 009 | [Create ADR 0007: Separation of Matching Logic into Matcher Module](009-create-adr-0007-matcher-module.md) | Medium | enhancement, documentation, architecture | Documentation |
| 010 | [Update ADR 0005: Add AgentPipeline injection into Handler](010-update-adr-0005-di.md) | Medium | enhancement, documentation, architecture | Documentation |
| 011 | [Add SelectionMode enum to Question struct](011-add-selectionmode-enum.md) | Medium | enhancement, backend | Feature |
| 012 | [Create ADR 0008: Quality Assurance Approach](012-create-adr-0008-quality-assurance.md) | Medium | enhancement, documentation, architecture, testing | Documentation |
| 013 | [Add pre-commit hook for formatting, linting, and tests](013-add-precommit-hook.md) | Medium | enhancement, tooling, testing | Tooling |
| 014 | [Enforce 100% coverage in CI/CD](014-enforce-coverage-in-ci.md) | Medium | enhancement, ci, testing | Tooling |

## Implementation Order

### Phase 1: Domain Documentation (Can be done in parallel)
1. Issue 001 - Update CONTEXT.md: Non-GitHub User section
2. Issue 002 - Update CONTEXT.md: Pipeline steps standardization

### Phase 2: Core Functionality
3. Issue 003 - Implement creating_persona step
4. Issue 004 - Update templates for creating_persona message
5. Issue 005 - Add multi-select validation
6. Issue 006 - Return 400 for invalid multi-select

### Phase 3: Testing
7. Issue 007 - Add tests for 100% coverage

### Phase 4: Architecture Documentation
8. Issue 008 - Create ADR 0006 (LLM Fallbacks)
9. Issue 009 - Create ADR 0007 (Matcher Module)
10. Issue 010 - Update ADR 0005 (DI with AgentPipeline)
11. Issue 011 - Add SelectionMode enum

### Phase 5: Quality Assurance
12. Issue 012 - Create ADR 0008 (Quality Assurance)
13. Issue 013 - Add pre-commit hook
14. Issue 014 - Enforce coverage in CI

## How to Use

1. Copy the content of each issue file
2. Create a new issue in the GitHub repository
3. Paste the content
4. Add the specified labels
5. Set the priority

## Dependencies

- Issue 003 depends on Issues 001 and 002 (domain clarity first)
- Issue 004 depends on Issue 003 (implement step before updating display)
- Issue 006 depends on Issue 005 (validation before error handling)
- Issue 010 depends on implementing AgentPipeline injection in Handler
- Issue 013 and 014 depend on Issue 007 (tests must exist before enforcing coverage)
