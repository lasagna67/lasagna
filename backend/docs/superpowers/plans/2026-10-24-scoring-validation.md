# Scoring validation implementation plan

**Goal:** Validate saved company data before scoring.

**Architecture:** A validator calls CompanyService.SaveOverview, then CompanyRepo.GetOverview. Company membership supplies the authoritative INN. Invalid business data returns a blocked result with reasons; infrastructure failures return an error.

**Tech Stack:** Go, existing company domain and repository interfaces.

**Approved rules:** Only terminated blocks among known registry statuses. Fewer than 4 unique financial months blocks; 4–5 requires manual review; 6+ needs no additional review. Required missing or invalid data blocks. All specified BKI fields are checked; false is a valid has_credit_history value.

**Approach:** Reuse existing service and repository through small interfaces, rather than duplicating fetching or SQL. Change has_credit_history to *bool to preserve missing/null values through JSON persistence.

## Tasks

- [x] Add table tests for thresholds, registry status, BKI validation, missing data, INN mismatch, duplicate months and operation failures/order.
- [x] Run tests to confirm the validator is missing.
- [x] Implement internal/scoring/validate.go and preserve required boolean presence in the company domain; adapt existing literal in repository test.
- [x] Run targeted tests, full backend tests and go vet; inspect diff.

## Review focus

Missing/null boolean versus explicit false; repeated loans/months; future report dates; errors must stop subsequent operations; stored reports must match the requested company.

## Verification

Targeted scoring/company tests passed. Full backend `go test ./...` and `go vet ./...` passed with GOCACHE set to D:/alpha_hac_10_26/.go-cache-scoring after the default system cache returned access denied. No live upstream calls are needed by the validator tests; service/repository boundaries use in-memory test implementations.
