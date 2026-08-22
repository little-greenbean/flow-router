# NewAPI Session Refresh Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent NewAPI monitoring from leaking login sessions when refresh requests fail or overlap.

**Architecture:** The NewAPI connector will send the upstream origin on refresh and attach a typed fallback policy to refresh errors. The channel service will serialize session acquisition per channel and only fall back to password login when the connector explicitly permits it, while preserving existing Sub2API behavior for untyped errors.

**Tech Stack:** Go 1.23, Resty, `net/http/httptest`, GORM/SQLite test storage.

---

### Task 1: NewAPI refresh origin and fallback policy

**Files:**
- Modify: `backend/connector/connector.go`
- Create: `backend/connector/connector_test.go`
- Modify: `backend/connector/newapi/newapi.go`
- Modify: `backend/connector/newapi/newapi_test.go`

- [ ] **Step 1: Write failing connector tests**

  Extend `TestRefreshSessionPostsRefreshCookie` to require `Origin == srv.URL`. Add a table test that makes refresh return 401, 403, 409, 429, and 500, then asserts only 401 permits login fallback. Add an invalid-site test that passes `ftp://example.com/path` and asserts no request is sent.

- [ ] **Step 2: Run focused tests and verify RED**

  Run:

  ```bash
  go test ./backend/connector/newapi -run 'TestRefreshSession(PostsRefreshCookie|FallbackPolicy|RejectsInvalidSiteURL)' -count=1
  ```

  Expected: the Origin assertion fails and the fallback policy API is undefined.

- [ ] **Step 3: Add typed refresh error policy**

  Add a connector-level wrapper and query helper. Untyped errors must return `true` to preserve existing connector behavior:

  ```go
  type SessionRefreshError struct {
      Err                error
      AllowLoginFallback bool
  }

  func (e *SessionRefreshError) Error() string { return e.Err.Error() }
  func (e *SessionRefreshError) Unwrap() error { return e.Err }

  func WrapSessionRefreshError(err error, allowLoginFallback bool) error {
      return &SessionRefreshError{Err: err, AllowLoginFallback: allowLoginFallback}
  }

  func ShouldLoginAfterRefreshError(err error) bool {
      var refreshErr *SessionRefreshError
      if !errors.As(err, &refreshErr) {
          return true
      }
      return refreshErr.AllowLoginFallback
  }
  ```

- [ ] **Step 4: Add NewAPI origin and error classification**

  Parse `site_url` with `net/url`, accept only HTTP/HTTPS with a non-empty host, and return `scheme://host`. Set that value as the request `Origin`. Wrap network, invalid URL, 403, 409, 429, 5xx, decode, and protocol errors with `AllowLoginFallback=false`; wrap HTTP 401 with `AllowLoginFallback=true`.

- [ ] **Step 5: Verify GREEN and commit**

  Run:

  ```bash
  go test ./backend/connector ./backend/connector/newapi -count=1
  ```

  Expected: both packages pass.

  Commit:

  ```bash
  git add backend/connector/connector.go backend/connector/connector_test.go backend/connector/newapi/newapi.go backend/connector/newapi/newapi_test.go
  git commit -m "fix: make NewAPI refresh origin-safe"
  ```

### Task 2: Safe channel refresh fallback and serialization

**Files:**
- Modify: `backend/channel/service.go`
- Modify: `backend/channel/service_test.go`

- [ ] **Step 1: Write failing channel tests**

  Add three integration-style service tests using `httptest.Server` and the real NewAPI connector:

  - A stored near-expiry session receives refresh HTTP 403; assert `EnsureSession` returns the refresh error, the login handler is never called, and the stored session remains.
  - A stored near-expiry session receives refresh HTTP 401; assert one password login occurs and the new session is persisted.
  - Two concurrent `EnsureSession` calls start from the same near-expiry session; block the first refresh briefly, start the second call, and assert the server receives one refresh while both callers receive the rotated access token.

- [ ] **Step 2: Run focused tests and verify RED**

  Run:

  ```bash
  go test ./backend/channel -run 'TestEnsureSession(NewAPIRefreshFailureDoesNotLogin|NewAPIUnauthorizedRefreshRelogsIn|SerializesRefreshByChannel)' -count=1
  ```

  Expected: 403 triggers a login attempt and concurrent refresh count is greater than one.

- [ ] **Step 3: Serialize `EnsureSession` per channel**

  Add `sessionLocks sync.Map` to `Service` and acquire a `*sync.Mutex` keyed by `c.ID` at the start of `EnsureSession`:

  ```go
  func (s *Service) sessionLock(channelID uint) *sync.Mutex {
      lock, _ := s.sessionLocks.LoadOrStore(channelID, &sync.Mutex{})
      return lock.(*sync.Mutex)
  }
  ```

  Keep different channels concurrent and keep the lock through read, refresh/login, and persistence.

- [ ] **Step 4: Honor refresh fallback policy**

  In `refreshStoredSession`, return a typed non-fallback error without deleting the cached session or calling login. Keep the current fallback path for 401 and untyped Sub2API errors. Record the returned error in `channels.last_error` so the dashboard exposes the actual failure.

- [ ] **Step 5: Verify GREEN and commit**

  Run:

  ```bash
  go test ./backend/channel -count=1
  ```

  Expected: all channel tests pass.

  Commit:

  ```bash
  git add backend/channel/service.go backend/channel/service_test.go
  git commit -m "fix: prevent repeated session login fallback"
  ```

### Task 3: Full verification and local startup

**Files:**
- No production files expected.

- [ ] **Step 1: Format and inspect the diff**

  Run:

  ```bash
  gofmt -w backend/connector/connector.go backend/connector/connector_test.go backend/connector/newapi/newapi.go backend/connector/newapi/newapi_test.go backend/channel/service.go backend/channel/service_test.go
  git diff --check
  git diff origin/main...HEAD --stat
  ```

- [ ] **Step 2: Run full backend verification**

  Run:

  ```bash
  go test ./...
  go vet ./...
  ```

  Expected: both commands exit 0 with no failed package.

- [ ] **Step 3: Start the local project**

  Check whether ports 8418 and 3010 are free. Start the backend with a temporary local data directory and non-production secret:

  ```bash
  APP_SECRET=local-development-session-refresh-secret DATABASE_DRIVER=sqlite DATABASE_PATH=<temporary-dir>/upstream-ops.db AUTH_ENABLED=false SERVER_PORT=8418 go run ./cmd/server
  ```

  Start the frontend with `pnpm --dir frontend install` if dependencies are missing, then `pnpm --dir frontend dev --host 127.0.0.1`. If either default port is occupied, select a free alternate and report it.

- [ ] **Step 4: Verify local health**

  Request backend `/healthz` and the frontend root. Expected: backend returns 200 healthy status, frontend returns HTTP 200, and startup logs contain no panic or migration error.
