#!/usr/bin/env bash
#
# Loadstar security dogfooding suite — probes the findings from the July 2026
# security audit end-to-end against a real daemon. Each probe is written to
# FAIL on vulnerable code and PASS once the fix is in, so the suite doubles as
# a demonstration of the hole and a regression gate.
#
# Usage:
#   scripts/dogfood_security.sh
#
# Re-runnable and self-cleaning. Exits non-zero if any assertion fails.
# Requires: go, curl. No Chrome and no outbound network needed.

set -uo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

WORK="$(mktemp -d)"
API=18090           # daemon, SSRF guard ON (rejection tests)
API_LH=18091        # daemon, private IPs allowed (lighthouse leak test)
KEY="dogfood-sec-secret"
GKEY="DOGFOOD-GOOGLE-API-KEY-SHOULD-NEVER-LEAK"
PASS=0; FAIL=0
declare -a PIDS=()

pass(){ echo "  ok   - $1"; PASS=$((PASS+1)); }
fail(){ echo "  FAIL - $1"; FAIL=$((FAIL+1)); }
assert_code(){ # $1=desc $2=expected $3=actual
  if [ "$2" = "$3" ]; then pass "$1 ($3)"; else fail "$1 (want $2 got $3)"; fi
}
assert_has(){ # $1=desc $2=needle $3=haystack
  case "$3" in *"$2"*) pass "$1";; *) fail "$1 (missing '$2')";; esac
}
assert_lacks(){ # $1=desc $2=needle $3=haystack
  case "$3" in *"$2"*) fail "$1 (found '$2')";; *) pass "$1";; esac
}
hcode(){ curl -s -o /dev/null -w '%{http_code}' "$@"; }

cleanup(){
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null; done
  pkill -x loadstar 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "== build =="
go build -o bin/loadstar ./cmd/loadstar || { echo "build loadstar failed"; exit 2; }

# Fixture: a plain page for the network tier to hit.
cat > "$WORK/fixture.go" <<'GO'
package main

import ("fmt"; "net/http"; "os")

func main() {
	http.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><h1>sec</h1></body></html>`)
	})
	http.ListenAndServe(os.Args[1], nil)
}
GO

# Slowloris probe: opens a connection, sends a partial request line and never
# finishes the headers, then waits to see whether the server hangs up.
# Prints "CLOSED <n>s" if the server dropped it, "HELD" if it did not.
cat > "$WORK/slowloris.go" <<'GO'
package main

import ("fmt"; "net"; "os"; "strconv"; "time")

func main() {
	addr, waitS := os.Args[1], os.Args[2]
	limit, _ := strconv.Atoi(waitS)
	conn, err := net.Dial("tcp", addr)
	if err != nil { fmt.Println("DIALFAIL"); return }
	defer conn.Close()
	// A request line and one header, but never the blank line that ends them.
	fmt.Fprint(conn, "GET /v1/health HTTP/1.1\r\nHost: localhost\r\nX-Slow: ")
	start := time.Now()
	conn.SetReadDeadline(time.Now().Add(time.Duration(limit) * time.Second))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	elapsed := int(time.Since(start).Seconds())
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			fmt.Println("HELD")   // server never hung up -> Slowloris works
			return
		}
		fmt.Printf("CLOSED %ds\n", elapsed) // EOF/reset -> server enforced a timeout
		return
	}
	fmt.Printf("CLOSED %ds\n", elapsed) // server replied (408) and closed
}
GO

( cd "$WORK" && go mod init sec >/dev/null 2>&1; go build -o fixture fixture.go && go build -o slowloris slowloris.go ) \
  || { echo "probe build failed"; exit 2; }

FIXPORT=18099
"$WORK/fixture" 127.0.0.1:$FIXPORT >/dev/null 2>&1 & PIDS+=($!)

echo "== start daemons =="
# Guard ON: used for the SSRF allow-list probes.
LOADSTAR_API_KEY="$KEY" LOADSTAR_SCHEDULER_ENABLED=false \
  DATABASE_URL="$WORK/sec.db" LOADSTAR_LISTEN_ADDR=":$API" \
  ./bin/loadstar serve >"$WORK/api.log" 2>&1 & PIDS+=($!)

# Private IPs allowed + HTTPS proxied to a dead port, so the Lighthouse tier's
# call to googleapis.com fails at the transport layer deterministically. That
# is the error path that embeds the PSI query string (and therefore the key).
LOADSTAR_API_KEY="$KEY" LOADSTAR_SCHEDULER_ENABLED=false \
  LOADSTAR_ALLOW_PRIVATE_IPS=true LOADSTAR_GOOGLE_API_KEY="$GKEY" \
  HTTPS_PROXY="http://127.0.0.1:1" \
  DATABASE_URL="$WORK/sec_lh.db" LOADSTAR_LISTEN_ADDR=":$API_LH" \
  ./bin/loadstar serve >"$WORK/api_lh.log" 2>&1 & PIDS+=($!)
sleep 2

echo
echo "== SEC-1: PSI API key must not leak into job errors =="
JOB=$(curl -s -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API_LH/v1/jobs" \
      -d "{\"url\":\"http://127.0.0.1:$FIXPORT/page\",\"tiers\":[\"lighthouse\"],\"runs\":1,\"timeout_s\":20}" \
      | sed -n 's/.*"job_id":"\([^"]*\)".*/\1/p')
if [ -z "$JOB" ]; then
  fail "SEC-1 submit lighthouse job (no job_id returned)"
else
  for _ in $(seq 1 30); do
    BODY=$(curl -s -H "X-API-Key: $KEY" "http://127.0.0.1:$API_LH/v1/jobs/$JOB")
    case "$BODY" in *'"status":"FAILED"'*|*'"status":"PARTIAL"'*|*'"status":"COMPLETED"'*) break;; esac
    sleep 1
  done
  assert_lacks "SEC-1 job error does not contain the Google API key" "$GKEY" "$BODY"
  assert_lacks "SEC-1 daemon log does not contain the Google API key" "$GKEY" "$(cat "$WORK/api_lh.log")"
fi

echo
echo "== SEC-2: SSRF allow-list covers CGNAT / RFC-6598 space =="
for ip in 100.100.100.200 100.64.0.1 198.18.0.1 192.0.0.1; do
  code=$(hcode -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API/v1/jobs" \
         -d "{\"url\":\"http://$ip/latest/meta-data/\",\"tiers\":[\"network\"]}")
  assert_code "SEC-2 job targeting $ip rejected" 400 "$code"
done
# webhook_url takes the same path and must be blocked too
code=$(hcode -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API/v1/jobs" \
       -d "{\"url\":\"http://93.184.216.34/\",\"tiers\":[\"network\"],\"webhook_url\":\"http://100.100.100.200/x\"}")
assert_code "SEC-2 webhook_url targeting 100.100.100.200 rejected" 400 "$code"
# schedules share the validator
code=$(hcode -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API/v1/schedules" \
       -d "{\"url\":\"http://100.64.0.1/\",\"tiers\":[\"network\"],\"interval_seconds\":60}")
assert_code "SEC-2 schedule targeting 100.64.0.1 rejected" 400 "$code"
# regression: the ranges that already worked must keep working
for ip in 169.254.169.254 127.0.0.1 10.0.0.1; do
  code=$(hcode -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API/v1/jobs" \
         -d "{\"url\":\"http://$ip/\",\"tiers\":[\"network\"]}")
  assert_code "SEC-2 (regression) $ip still rejected" 400 "$code"
done

echo
echo "== SEC-3: HTTP server enforces header/read timeouts (Slowloris) =="
RES=$("$WORK/slowloris" "127.0.0.1:$API" 20)
case "$RES" in
  CLOSED*) pass "SEC-3 server closed a stalled-header connection ($RES)";;
  HELD)    fail "SEC-3 server held a stalled-header connection open (Slowloris)";;
  *)       fail "SEC-3 slowloris probe error ($RES)";;
esac

echo
echo "== SEC-4: /docs pins third-party assets (SRI) and responses carry CSP =="
DOCS=$(curl -s "http://127.0.0.1:$API/docs")
assert_has  "SEC-4 swagger css has integrity hash" 'integrity="sha384-' "$DOCS"
SRI_COUNT=$(printf '%s' "$DOCS" | grep -o 'integrity="sha384-' | wc -l | tr -d ' ')
if [ "$SRI_COUNT" -ge 2 ]; then pass "SEC-4 both swagger assets pinned ($SRI_COUNT)"; else fail "SEC-4 expected 2 SRI hashes, got $SRI_COUNT"; fi
HDRS=$(curl -s -D - -o /dev/null "http://127.0.0.1:$API/docs")
assert_has "SEC-4 /docs sends Content-Security-Policy"  "Content-Security-Policy" "$HDRS"
assert_has "SEC-4 /docs sends X-Content-Type-Options"   "X-Content-Type-Options"  "$HDRS"
UIH=$(curl -s -D - -o /dev/null "http://127.0.0.1:$API/")
assert_has  "SEC-4 status page sends Content-Security-Policy" "Content-Security-Policy" "$UIH"
assert_lacks "SEC-4 status page CSP has no unsafe-inline"     "unsafe-inline"           "$UIH"

echo
echo "== SEC-5: identifiers carry full UUID entropy =="
SJ=$(curl -s -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API/v1/schedules" \
     -d "{\"url\":\"http://93.184.216.34/\",\"tiers\":[\"network\"],\"interval_seconds\":60}" \
     | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
# "sc_" + 36-char UUID = 39; the vulnerable version produced "sc_" + 8 = 11.
LEN=${#SJ}
if [ "$LEN" -ge 39 ]; then pass "SEC-5 schedule id has full UUID entropy (len=$LEN)"; else fail "SEC-5 schedule id truncated (len=$LEN, want >=39: '$SJ')"; fi
JID=$(curl -s -H "X-API-Key: $KEY" -X POST "http://127.0.0.1:$API_LH/v1/jobs" \
      -d "{\"url\":\"http://127.0.0.1:$FIXPORT/page\",\"tiers\":[\"network\"]}" \
      | sed -n 's/.*"job_id":"\([^"]*\)".*/\1/p')
LEN=${#JID}
if [ "$LEN" -ge 39 ]; then pass "SEC-5 job id has full UUID entropy (len=$LEN)"; else fail "SEC-5 job id truncated (len=$LEN, want >=39: '$JID')"; fi

echo
echo "== SEC-6: RUM ingest bounds the url label =="
# Restart the guard-ON daemon with RUM enabled (PIDS[0] is the fixture).
kill "${PIDS[1]}" 2>/dev/null; sleep 1
LOADSTAR_API_KEY="$KEY" LOADSTAR_SCHEDULER_ENABLED=false LOADSTAR_RUM_ORIGINS="*" \
  DATABASE_URL="$WORK/sec.db" LOADSTAR_LISTEN_ADDR=":$API" \
  ./bin/loadstar serve >>"$WORK/api.log" 2>&1 & PIDS+=($!)
sleep 2
LONG=$(printf 'a%.0s' $(seq 1 5000))
code=$(hcode -X POST "http://127.0.0.1:$API/v1/rum" \
       -d "{\"url\":\"https://example.com/$LONG\",\"name\":\"LCP\",\"value\":1200}")
assert_code "SEC-6 over-long RUM url rejected" 400 "$code"
code=$(hcode -X POST "http://127.0.0.1:$API/v1/rum" \
       -d '{"url":"https://example.com/ok","name":"LCP","value":1200}')
assert_code "SEC-6 (regression) normal RUM beacon accepted" 204 "$code"

echo
echo "=================================="
echo " passed: $PASS   failed: $FAIL"
echo "=================================="
[ "$FAIL" -eq 0 ] || exit 1
