#!/usr/bin/env python3
"""Writes the CI logs of the ci-log-triage task, deterministically.

python3 gen.py DIR    write DIR/job-NNNN.log for 40 failed jobs
python3 gen.py --truth print the expected triage as CSV: job,cause,test,commit
"""
import random
import sys
from datetime import datetime, timedelta

MOD = "example.com/ledger"
PKGS = {
    "internal/api": ["TestRouter", "TestCreateInvoiceHandler", "TestListInvoices", "TestAuthMiddleware", "TestRateLimit", "TestHealthz", "TestPagination", "TestErrorEnvelope"],
    "internal/auth": ["TestTokenIssue", "TestTokenRefresh", "TestScopes", "TestPasswordHash", "TestSessionExpiry", "TestAPIKeyRotation"],
    "internal/billing": ["TestPlanChange", "TestProration", "TestUsageAggregation", "TestTrialEnd", "TestCreditApply", "TestDunningSchedule", "TestCouponStacking"],
    "internal/fx": ["TestConvert", "TestRateCache", "TestRoundingModes", "TestStaleRates", "TestCrossRates"],
    "internal/invoice": ["TestInvoiceTotals", "TestLineItems", "TestInvoiceNumbering", "TestPDFRender", "TestDueDates", "TestVoidInvoice", "TestCreditNote"],
    "internal/ledger": ["TestPostEntry", "TestBalance", "TestDoubleEntryInvariant", "TestReversal", "TestPeriodClose", "TestTrialBalance"],
    "internal/notify": ["TestEmailTemplate", "TestWebhookSign", "TestWebhookRetry", "TestDigest", "TestUnsubscribe"],
    "internal/payments": ["TestCharge", "TestRefund", "TestIdempotencyKey", "TestCardDecline", "TestSettlement", "TestChargeback", "TestPayoutSchedule"],
    "internal/queue": ["TestEnqueue", "TestLease", "TestQueueDrain", "TestDeadLetter", "TestPriority", "TestVisibilityTimeout"],
    "internal/report": ["TestMonthlyRevenue", "TestAgingReport", "TestCSVExport", "TestCohorts", "TestLargeExport"],
    "internal/store/pg": ["TestMigrate", "TestTxRetry", "TestInvoiceRepo", "TestLedgerRepo", "TestAdvisoryLock", "TestConnPool"],
    "internal/tax": ["TestVATRates", "TestReverseCharge", "TestUSSalesTax", "TestTaxExempt", "TestRoundingPerLine"],
    "cmd/ledgerd": ["TestFlags", "TestGracefulShutdown", "TestConfigLoad"],
}
SUBS = ["basic", "empty", "zero_amount", "negative", "large_values", "unicode_names", "eur", "usd", "jpy", "leap_year", "month_end", "concurrent", "idempotent", "retry", "partial", "nil_input"]
AUTHORS = ["Priya N.", "Tomas K.", "Ada W.", "Lin Q.", "Marek S.", "Fatima R.", "Jonas B.", "Keiko M."]

# Regressions: (package, test, subtest, failure lines, culprit subject, decoy subject in the same package)
REGRESSIONS = [
    ("internal/invoice", "TestInvoiceTotals", "half_cent_amounts",
     ["invoice_test.go:88: total for 3 x 0.335 EUR: got 1.00, want 1.01", "invoice_test.go:89: rounding mode: HalfEven"],
     "invoice: switch line totals to banker's rounding", "invoice: rename PDF template field due_on"),
    ("internal/tax", "TestReverseCharge", "eu_b2b_services",
     ["tax_test.go:142: reverse charge for DE->FR B2B services: got VAT 20.00, want 0.00", "tax_test.go:143: customer VAT ID was present: FR40303265045"],
     "tax: look up VAT rate by shipping country first", "tax: add 2027 rate table for Estonia"),
    ("internal/billing", "TestProration", "downgrade_mid_cycle",
     ["billing_test.go:211: credit for downgrade on day 15 of 30: got 0.00, want 50.00"],
     "billing: skip proration when the new plan is cheaper", "billing: log plan id in dunning emails"),
    ("internal/ledger", "TestPeriodClose", "late_entry",
     ["ledger_test.go:77: entry dated 2026-08-31T23:59:59Z posted after close: want ErrPeriodClosed, got <nil>"],
     "ledger: compare period end in local time", "ledger: speed up trial balance with a single query"),
    ("internal/payments", "TestIdempotencyKey", "same_key_different_amount",
     ["payments_test.go:301: second charge with key idem-42 and amount 19.99 (first 9.99): want ErrKeyReused, got charge ch_8812"],
     "payments: hash idempotency keys without the amount", "payments: bump settlement batch size to 500"),
    ("internal/fx", "TestRoundingModes", "jpy_no_minor_units",
     ["fx_test.go:56: convert 10.00 USD to JPY: got 1493.27, want 1493", "fx_test.go:57: JPY has 0 minor units"],
     "fx: use two decimals for every currency in Convert", "fx: refresh rate cache every 30 minutes"),
    ("internal/api", "TestPagination", "cursor_last_page",
     ["api_test.go:410: GET /v1/invoices?cursor=eyJpZCI6OTl9: next_cursor = \"eyJpZCI6OTl9\", want \"\" on the last page"],
     "api: always return next_cursor in list responses", "api: document the 429 response body"),
    ("internal/auth", "TestSessionExpiry", "sliding_window",
     ["auth_test.go:133: session last seen 29m ago with 30m sliding window: got expired, want valid"],
     "auth: measure session expiry from creation time", "auth: add scopes to the token introspection output"),
]
FLAKES = [
    ("internal/store/pg", "TestTxRetry", ["pg_test.go:64: begin: dial tcp 10.1.0.12:5432: i/o timeout"]),
    ("internal/notify", "TestWebhookRetry", ["notify_test.go:98: POST http://hooks.ci.internal:8099/ok: read tcp 10.1.0.33:51144->10.1.0.40:8099: read: connection reset by peer"]),
    ("internal/queue", "TestLease", ["queue_test.go:51: redis: dial tcp 10.1.0.15:6379: connect: connection refused"]),
    ("internal/payments", "TestSettlement", ["payments_test.go:188: sandbox gateway: Post \"https://sandbox.payments.ci.internal/v2/settle\": net/http: TLS handshake timeout"]),
]
DEADLOCKS = [("internal/queue", "TestQueueDrain"), ("internal/store/pg", "TestAdvisoryLock"), ("cmd/ledgerd", "TestGracefulShutdown"), ("internal/notify", "TestDigest")]
OOMS = ["internal/report", "internal/ledger", "internal/billing"]
OTHER_SUBJECTS = [
    "ci: pin golangci-lint to v2.4", "docs: explain the invoice state machine", "deps: bump golang.org/x/text to v0.29.0",
    "api: add request id to access logs", "notify: trim whitespace in email subjects", "store/pg: add index on invoices(customer_id)",
    "report: stream CSV rows instead of buffering", "queue: export lease metrics", "cmd/ledgerd: print version on start",
    "billing: typo in trial reminder copy", "ledger: comment the reversal rules", "tax: move rate tables to embed",
    "payments: retry webhook delivery on 502", "auth: lower bcrypt cost in tests", "fx: log provider latency",
]

CAUSES = ["regression"] * 12 + ["flaky-network"] * 8 + ["oom-killed"] * 6 + ["disk-full"] * 5 + ["deadlock-timeout"] * 5 + ["infra"] * 4


def plan():
    rnd = random.Random(20260930)
    # a commit has one hash in every job that lists it
    hashes = {}

    def hash_of(subject):
        if subject not in hashes:
            hashes[subject] = "%07x" % rnd.getrandbits(28)
        return hashes[subject]

    causes = CAUSES[:]
    rnd.shuffle(causes)
    jobs = []
    t0 = datetime(2026, 9, 24, 1, 0, 0)
    num = 4101
    for i, cause in enumerate(causes):
        num += rnd.randint(1, 9)
        start = t0 + timedelta(hours=4 * i, minutes=rnd.randint(0, 59), seconds=rnd.randint(0, 59))
        job = {"num": num, "cause": cause, "start": start, "test": "", "commit": "", "seed": rnd.randint(0, 1 << 30)}
        subjects = rnd.sample(OTHER_SUBJECTS, rnd.randint(2, 5))
        if cause == "regression":
            job["reg"] = REGRESSIONS[i % len(REGRESSIONS)] if i % 3 else REGRESSIONS[rnd.randrange(len(REGRESSIONS))]
            job["test"] = job["reg"][1]
            culprit, decoy = job["reg"][4], job["reg"][5]
            subjects += [culprit, decoy]
        elif cause == "flaky-network":
            job["flake"] = rnd.choice(FLAKES)
            job["test"] = job["flake"][1]
            pkg = job["flake"][0].split("/")[-1]
            subjects.append(next(s for s in OTHER_SUBJECTS + [f"{pkg}: tidy test helpers"] if s.startswith(pkg + ":")))
        elif cause == "deadlock-timeout":
            job["dead"] = rnd.choice(DEADLOCKS)
            job["test"] = job["dead"][1]
        elif cause == "oom-killed":
            job["oom"] = rnd.choice(OOMS)
        rnd.shuffle(subjects)
        commits = []
        for s in subjects:
            h = hash_of(s)
            commits.append((h, s, rnd.choice(AUTHORS)))
            if job["cause"] == "regression" and s == job["reg"][4]:
                job["commit"] = h
        job["commits"] = commits
        # every job carries a recovered flake, and some a disk warning after the tests
        job["herring_flake"] = rnd.choice(FLAKES)
        job["herring_disk"] = cause != "disk-full" and rnd.random() < 0.5
        job["herring_pull"] = cause != "infra" and rnd.random() < 0.5
        job["arch"] = rnd.choice(["linux-amd64", "linux-arm64"])
        jobs.append(job)
    return jobs


class Log:
    def __init__(self, start, rnd):
        self.t = start
        self.rnd = rnd
        self.lines = []

    def w(self, s="", dt=None):
        self.t += timedelta(milliseconds=dt if dt is not None else self.rnd.randint(1, 40))
        self.lines.append(self.t.strftime("%Y-%m-%dT%H:%M:%S.") + "%03dZ " % (self.t.microsecond // 1000) + s)


def pkg_tests(L, rnd, pkg, fail=None, flake_retry=None):
    """Writes go test -v output for one package; fail is (test, sub, lines)."""
    tests = PKGS[pkg]
    for t in tests:
        L.w(f"=== RUN   {t}")
        L.w(f"=== PAUSE {t}")
    for t in tests:
        L.w(f"=== CONT  {t}")
    failed = False
    for t in tests:
        subs = rnd.sample(SUBS, rnd.randint(3, 9))
        if fail and fail[0] == t and fail[1] not in subs:
            subs[rnd.randrange(len(subs))] = fail[1]
        for s in subs:
            L.w(f"=== RUN   {t}/{s}")
        bad = False
        for s in subs:
            d = rnd.uniform(0, 0.09)
            if fail and fail[0] == t and fail[1] == s:
                for line in fail[2]:
                    L.w(f"    {line}")
                L.w(f"    --- FAIL: {t}/{s} ({d:.2f}s)")
                bad = True
            else:
                L.w(f"    --- PASS: {t}/{s} ({d:.2f}s)")
        if bad:
            L.w(f"--- FAIL: {t} ({rnd.uniform(0.1, 1.5):.2f}s)")
            failed = True
        else:
            L.w(f"--- PASS: {t} ({rnd.uniform(0.01, 1.5):.2f}s)")
        if flake_retry and flake_retry[1] == t:
            # a flake that passed on its rerun
            L.w(f"    {flake_retry[2][0]}")
            L.w(f"--- FAIL: {t} ({rnd.uniform(5, 10):.2f}s)")
            L.w(f"=== RERUN {t} (attempt 2 of 3, gotestsum --rerun-fails)")
            L.w(f"--- PASS: {t} ({rnd.uniform(0.1, 1):.2f}s)")
    if failed:
        L.w("FAIL")
        L.w(f"FAIL\t{MOD}/{pkg}\t{rnd.uniform(1, 30):.3f}s")
    else:
        L.w("PASS")
        L.w(f"ok  \t{MOD}/{pkg}\t{rnd.uniform(0.2, 12):.3f}s")
    return failed


def goroutine_dump(L, rnd, pkg, test):
    L.w(f"panic: test timed out after 10m0s")
    L.w("running tests:")
    L.w(f"\t{test} (10m0s)")
    L.w("")
    funcs = {
        "TestQueueDrain": ("queue.(*Queue).Drain", "queue.go", "sync.(*WaitGroup).Wait"),
        "TestAdvisoryLock": ("pg.(*Store).WithLock", "lock.go", "sync.(*Mutex).Lock"),
        "TestGracefulShutdown": ("main.(*server).shutdown", "main.go", "sync.(*WaitGroup).Wait"),
        "TestDigest": ("notify.(*Digest).Flush", "digest.go", "sync.(*Mutex).Lock"),
    }[test]
    for g in range(rnd.randint(28, 40)):
        state = rnd.choice(["chan receive", "select", "semacquire", "IO wait", "sync.Mutex.Lock", "sleep"])
        L.w(f"goroutine {rnd.randint(1, 900)} [{state}, {rnd.randint(1, 9)} minutes]:", dt=0)
        if g == 0:
            L.w(f"{funcs[2]}(0xc000{rnd.randint(100000, 999999)})", dt=0)
            L.w(f"\t/usr/local/go/src/sync/waitgroup.go:118 +0x48", dt=0)
            L.w(f"{MOD}/{pkg}.{funcs[0]}(0xc000{rnd.randint(100000, 999999)})", dt=0)
            L.w(f"\t/home/runner/work/ledger/ledger/{pkg}/{funcs[1]}:{rnd.randint(40, 300)} +0x{rnd.randint(16, 400):x}", dt=0)
        for _ in range(rnd.randint(3, 8)):
            fn = rnd.choice(["runtime.gopark", "runtime.chanrecv1", "runtime.selectgo", "internal/poll.(*FD).Read", "net.(*conn).Read", "bufio.(*Reader).fill",
                             "database/sql.(*DB).connectionOpener", "net/http.(*persistConn).readLoop", "time.Sleep", f"{MOD}/{pkg}.(*worker).loop"])
            L.w(f"{fn}(0xc000{rnd.randint(100000, 999999)}, 0x{rnd.randint(1, 255):x})", dt=0)
            L.w(f"\t/usr/local/go/src/runtime/proc.go:{rnd.randint(300, 4500)} +0x{rnd.randint(16, 400):x}", dt=0)
        L.w(f"created by {MOD}/{pkg}.New in goroutine {rnd.randint(1, 30)}", dt=0)
        L.w("", dt=0)
    L.w(f"FAIL\t{MOD}/{pkg}\t600.{rnd.randint(100, 999)}s")


def write_job(job):
    rnd = random.Random(job["seed"])
    L = Log(job["start"], rnd)
    num, cause = job["num"], job["cause"]
    L.w(f"##[group]Job nightly/test ({job['arch']}, go1.24.7) #{num}")
    L.w(f"Runner: ci-runner-{rnd.randint(1, 40):02d} (ubuntu-24.04, 8 vCPU, 16 GiB RAM, 60 GiB disk)")
    L.w(f"Workflow: .github/workflows/nightly.yml@refs/heads/main")
    L.w("##[endgroup]")
    L.w(f"Changes since the last green run (main@{'%07x' % rnd.getrandbits(28)}):")
    for h, s, a in job["commits"]:
        L.w(f"  {h} {s} ({a})")
    L.w("##[group]Checkout")
    for i in range(rnd.randint(8, 14)):
        L.w(f"remote: Counting objects: {min(100, i * 9)}% ({i * 512}/{4608})")
    L.w("HEAD is now at %07x" % rnd.getrandbits(28))
    L.w("##[endgroup]")
    L.w("##[group]Restore Go cache")
    L.w(f"Cache restored from key: go-{job['arch']}-{'%016x' % rnd.getrandbits(64)} ({rnd.randint(400, 1900)} MB)")
    L.w("##[endgroup]")
    L.w("##[group]Start services (postgres:16, redis:7, mock-gateway:3.2)")
    for img in ["postgres:16", "redis:7", "ghcr.io/ledger-ci/mock-gateway:3.2"]:
        if (job["herring_pull"] and img == "redis:7") or (cause == "infra" and job["seed"] % 2 == 0 and img.startswith("ghcr")):
            tries = 3 if cause == "infra" else 1
            for k in range(tries):
                L.w(f"Error response from daemon: toomanyrequests: You have reached your pull rate limit. (attempt {k + 1}/3, retrying in 15s)", dt=15000)
            if cause == "infra":
                L.w(f"##[error]Docker pull failed for {img} after 3 attempts")
                L.w("##[endgroup]")
                L.w("##[group]Post job cleanup")
                L.w("Stopping containers: postgres, redis")
                L.w("##[endgroup]")
                L.w("##[error]Process completed with exit code 1.")
                return "\n".join(L.lines) + "\n"
        for layer in range(rnd.randint(4, 9)):
            L.w(f"{'%012x' % rnd.getrandbits(48)}: Pull complete")
        L.w(f"Status: Downloaded newer image for {img}")
    L.w("##[endgroup]")
    L.w("##[group]Build")
    L.w("go build -trimpath ./...")
    for i in range(rnd.randint(10, 20)):
        L.w(f"go: downloading {rnd.choice(['github.com/jackc/pgx/v5 v5.7.2', 'github.com/redis/go-redis/v9 v9.7.0', 'golang.org/x/text v0.29.0', 'github.com/google/uuid v1.6.0', 'go.opentelemetry.io/otel v1.34.0'])}")
    if cause == "disk-full":
        where = rnd.choice(["link", "cache"])
        if where == "link":
            L.w(f"# {MOD}/cmd/ledgerd")
            L.w(f"/usr/local/go/pkg/tool/linux_amd64/link: flushing $WORK/b001/exe/a.out: write $WORK/b001/exe/a.out: no space left on device")
        else:
            L.w(f"go: failed to trim cache: open /home/runner/.cache/go-build/{'%02x' % rnd.getrandbits(8)}/{'%064x' % rnd.getrandbits(256)}-d: no space left on device")
            L.w(f"# {MOD}/internal/report")
            L.w(f"compile: writing output: write $WORK/b{rnd.randint(100, 400)}/_pkg_.a: no space left on device")
        L.w("##[error]Build failed")
        L.w("##[endgroup]")
        L.w("##[group]Runner diagnostics")
        L.w("Filesystem      Size  Used Avail Use% Mounted on")
        L.w("/dev/root        60G   60G     0 100% /")
        L.w("##[endgroup]")
        L.w("##[error]Process completed with exit code 1.")
        return "\n".join(L.lines) + "\n"
    L.w("##[endgroup]")
    L.w("##[group]Test")
    L.w("gotestsum --rerun-fails=2 --format standard-verbose -- -race -timeout 10m ./...")
    pkgs = list(PKGS)
    rnd.shuffle(pkgs)
    stop_at = None
    if cause == "infra":
        stop_at = rnd.randint(3, 8)
    herring = job["herring_flake"]
    for idx, pkg in enumerate(pkgs):
        if stop_at is not None and idx == stop_at:
            L.w("##[error]The runner has received a shutdown signal. This can happen when the runner service is stopped, or a manually started runner is canceled.", dt=4000)
            L.w("##[error]The operation was canceled.")
            L.w("##[group]Post job cleanup")
            L.w("##[endgroup]")
            L.w("##[error]Process completed with exit code 143.")
            return "\n".join(L.lines) + "\n"
        retry = herring if herring[0] == pkg and cause != "flaky-network" else None
        if cause == "regression" and job["reg"][0] == pkg:
            r = job["reg"]
            pkg_tests(L, rnd, pkg, fail=(r[1], r[2], r[3]), flake_retry=retry)
            L.w(f"=== RERUN {r[1]} (attempt 2 of 3, gotestsum --rerun-fails)")
            L.w(f"    {r[3][0]}")
            L.w(f"--- FAIL: {r[1]} ({rnd.uniform(0.1, 1.5):.2f}s)")
            L.w(f"=== RERUN {r[1]} (attempt 3 of 3, gotestsum --rerun-fails)")
            L.w(f"    {r[3][0]}")
            L.w(f"--- FAIL: {r[1]} ({rnd.uniform(0.1, 1.5):.2f}s)")
        elif cause == "flaky-network" and job["flake"][0] == pkg:
            f = job["flake"]
            pkg_tests(L, rnd, pkg, fail=(f[1], rnd.choice(SUBS), f[2]))
            for k in (2, 3):
                L.w(f"=== RERUN {f[1]} (attempt {k} of 3, gotestsum --rerun-fails)")
                L.w(f"    {f[2][0]}")
                L.w(f"--- FAIL: {f[1]} ({rnd.uniform(5, 10):.2f}s)")
        elif cause == "deadlock-timeout" and job["dead"][0] == pkg:
            for t in PKGS[pkg]:
                L.w(f"=== RUN   {t}")
                if t == job["dead"][1]:
                    break
                L.w(f"--- PASS: {t} ({rnd.uniform(0.01, 1.5):.2f}s)")
            goroutine_dump(L, rnd, pkg, job["dead"][1])
        elif cause == "oom-killed" and job["oom"] == pkg:
            for t in PKGS[pkg][:3]:
                L.w(f"=== RUN   {t}")
                L.w(f"--- PASS: {t} ({rnd.uniform(0.01, 1.5):.2f}s)")
            L.w(f"=== RUN   {PKGS[pkg][3]}")
            L.w("signal: killed", dt=40000)
            L.w(f"FAIL\t{MOD}/{pkg}\t{rnd.uniform(40, 90):.3f}s")
        else:
            pkg_tests(L, rnd, pkg, flake_retry=retry)
    L.w("")
    L.w(f"DONE {sum(len(v) for v in PKGS.values()) * 6} tests, 1 failure in {rnd.uniform(200, 590):.3f}s")
    L.w("##[error]Process completed with exit code 1.")
    L.w("##[endgroup]")
    L.w("##[group]Upload test artifacts (continue-on-error)")
    if job["herring_disk"]:
        L.w("tar: coverage.out: Cannot write: No space left on device")
        L.w("##[warning]Artifact upload failed: no space left on device; continuing")
    else:
        L.w(f"Uploaded coverage.out ({rnd.randint(200, 900)} KB)")
    L.w("##[endgroup]")
    L.w("##[group]Runner diagnostics")
    L.w(f"Memory: peak {rnd.randint(4, 11)}.{rnd.randint(0, 9)} GiB of 16 GiB")
    if cause == "oom-killed":
        p = job["oom"].split("/")[-1]
        L.w(f"[{rnd.randint(10000, 99999)}.{rnd.randint(100000, 999999)}] Memory cgroup out of memory: Killed process {rnd.randint(2000, 40000)} ({p}.test) total-vm:{rnd.randint(17, 22)}000000kB, anon-rss:{rnd.randint(15, 16)}800000kB")
    for _ in range(rnd.randint(3, 6)):
        L.w(f"[{rnd.randint(10000, 99999)}.{rnd.randint(100000, 999999)}] {rnd.choice(['eth0: renamed from veth3a1b2c', 'overlayfs: idmapped layers are currently not supported', 'docker0: port 2(veth9) entered forwarding state', 'EXT4-fs (sda1): re-mounted. Quota mode: none.'])}")
    L.w("##[endgroup]")
    return "\n".join(L.lines) + "\n"


def main():
    jobs = plan()
    if sys.argv[1:] == ["--truth"]:
        print("job,cause,test,commit")
        for j in jobs:
            print(f"{j['num']},{j['cause']},{j['test']},{j['commit']}")
        return
    out = sys.argv[1]
    for j in jobs:
        with open(f"{out}/job-{j['num']}.log", "w") as f:
            f.write(write_job(j))


if __name__ == "__main__":
    main()
