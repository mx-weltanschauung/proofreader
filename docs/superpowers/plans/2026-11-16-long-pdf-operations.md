# Long PDF Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let large PDF uploads and page preview requests return their result after more than 15 seconds.

**Architecture:** Use `http.ResponseController` in the two work handlers to extend connection deadlines to nine minutes, while keeping global server defaults. Reuse the middleware's existing `Unwrap` support.

**Tech Stack:** Go `net/http`, current API and middleware tests.

**Spec:** `docs/superpowers/specs/2026-11-16-long-pdf-operations-design.md`

## Task 1: Extend work file deadlines

- [x] Add `WorkHandler` tests with a deadline-recording writer: upload extends read and write deadlines; create-pages extends write deadline. Use a valid ID and early validation failure to avoid storage fixtures.
- [x] Run the two tests and confirm they fail because no deadlines are set.
- [x] Add a nine-minute duration and extend deadlines at the start of each handler. Log controller errors.
- [x] Run focused tests, `go test ./...`, and `go build -buildvcs=false ./cmd/server` (VCS stamping fails in this temporary worktree).
- [x] Review the diff and commit only the platform change and its tests.

## Review Focus

The upload deadline must cover multipart parsing and storage; the page creation deadline must precede its PDF processing. The test checks both handlers directly. Existing middleware tests cover controller propagation through gzip and logging wrappers. No schema change is needed.
