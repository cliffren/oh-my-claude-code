# Pane Isolated Selection Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow drag-select copy inside multiple panes while keeping each selection isolated to the pane where it started.

**Architecture:** Keep mouse routing pane-local and move selection logic behind a reusable controller/helper that each selectable pane can adopt. The active selection belongs to exactly one pane at a time; wheel events stay pane-local, and release copies only that pane's visible text.

**Tech Stack:** Go, Bubble Tea, bubbles/viewport, Lip Gloss, macOS clipboard integration via `pbcopy`.

---

## File Map

- Modify: `internal/tui/components/chat/list.go` - keep chat pane on the shared selection contract.
- Modify: `internal/tui/components/logs/details.go` - add pane-local selection and copy for log details.
- Modify: `internal/tui/components/dialog/permission.go` - add pane-local selection and copy for permission content viewport.
- Modify: `internal/tui/layout/container.go` - preserve local mouse coordinate routing into child panes.
- Modify: `internal/tui/layout/split.go` - preserve active-pane routing boundaries for mouse events.
- Modify: `internal/tui/components/chat/selection.go` - generalize reusable selection helpers for any viewport-like pane.
- Modify: `internal/tui/components/chat/clipboard.go` - keep pane-agnostic clipboard writing.
- Create/Modify: tests in `internal/tui/components/logs/`, `internal/tui/components/dialog/`, and `internal/tui/layout/`.

## Chunk 1: Generalize Reusable Pane Selection Helpers

### Task 1: Add failing tests for selection helpers usable outside chat

**Files:**
- Modify: `internal/tui/components/chat/selection_test.go`
- Modify: `internal/tui/components/chat/selection.go`

- [ ] **Step 1: Write the failing test**

Add a test that proves `selectionController` can start, clamp, and release against arbitrary pane regions and visible lines without any chat-specific assumptions.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/components/chat -run '^TestSelectionControllerSupportsGenericPaneRegion$'`
Expected: FAIL until helper surface is generalized.

- [ ] **Step 3: Write minimal implementation**

Refactor helper names or signatures only as much as needed so non-chat panes can call them directly.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/components/chat -run '^TestSelectionControllerSupportsGenericPaneRegion$'`
Expected: PASS.

## Chunk 2: Add Isolated Selection To Log Details Pane

### Task 2: Add failing logs selection tests

**Files:**
- Create or modify: `internal/tui/components/logs/details_test.go`
- Modify: `internal/tui/components/logs/details.go`

- [ ] **Step 1: Write the failing test**

Add tests that verify:
- left-drag inside log details starts selection
- release copies only log-details visible text
- wheel scrolling still updates the log viewport

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/components/logs -run '^TestDetailPaneSelectionLifecycle$'`
Expected: FAIL because the pane does not yet support selection.

- [ ] **Step 3: Write minimal implementation**

Add a local selection controller and clipboard writer to the log details pane; keep the selection bounded to its viewport.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/components/logs -run '^TestDetailPaneSelectionLifecycle$'`
Expected: PASS.

## Chunk 3: Add Isolated Selection To Permission Dialog Content Pane

### Task 3: Add failing permission dialog selection tests

**Files:**
- Create or modify: `internal/tui/components/dialog/permission_test.go`
- Modify: `internal/tui/components/dialog/permission.go`

- [ ] **Step 1: Write the failing test**

Add tests that verify drag-select inside the permission dialog content viewport copies only permission text and does not affect button selection behavior.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/components/dialog -run '^TestPermissionDialogContentSelection$'`
Expected: FAIL until selection is implemented there.

- [ ] **Step 3: Write minimal implementation**

Add selection handling only for the content viewport area; leave button navigation unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/components/dialog -run '^TestPermissionDialogContentSelection$'`
Expected: PASS.

## Chunk 4: Verify Cross-Pane Isolation Rules

### Task 4: Add failing pane-isolation routing tests

**Files:**
- Modify: `internal/tui/layout/mouse_test.go`
- Modify: `internal/tui/layout/split.go`
- Modify: `internal/tui/layout/container.go`

- [ ] **Step 1: Write the failing test**

Add a test that simulates selection starting in one pane and pointer motion crossing toward another pane, then asserts events stay bound to the original pane until release.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/layout -run '^TestSplitPaneKeepsDragBoundToOriginPane$'`
Expected: FAIL until drag ownership is preserved explicitly.

- [ ] **Step 3: Write minimal implementation**

If needed, extend split-pane mouse routing with active drag ownership while preserving local coordinate translation.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/layout -run '^TestSplitPaneKeepsDragBoundToOriginPane$'`
Expected: PASS.

## Chunk 5: Final Verification And Install

### Task 5: Run focused verification

**Files:**
- Test: `internal/tui/components/chat`
- Test: `internal/tui/components/logs`
- Test: `internal/tui/components/dialog`
- Test: `internal/tui/layout`

- [ ] **Step 1: Run focused tests**

Run: `go test ./internal/tui/components/chat ./internal/tui/components/logs ./internal/tui/components/dialog ./internal/tui/layout ./internal/tui`
Expected: PASS.

- [ ] **Step 2: Build the repo**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Reinstall local binary**

Run: `go build -o "/Users/rentao/.local/bin/omcc" . && /Users/rentao/.local/bin/omcc -v`
Expected: build succeeds and version prints.

- [ ] **Step 4: Manual interaction check**

Verify in `omcc`:
- chat pane drag-copy still works
- right/details pane drag-copy works
- permission dialog content drag-copy works
- wheel scroll remains local to hovered pane
- selections do not cross pane boundaries
