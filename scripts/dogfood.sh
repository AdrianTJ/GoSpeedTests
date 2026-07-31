#!/usr/bin/env bash
#
# Loadstar dogfooding smoke suite — drives the built binaries end-to-end
# against a local fixture server and asserts real behavior (status codes,
# payload fields, the SSRF guard, and the DELETE -> FK-cascade fix).
#
# Usage:
#   scripts/dogfood.sh              # network + full API surface (no Chrome needed)
#   scripts/dogfood.sh --with-chrome # also exercise the browser/vitals tiers
#
# Re-runnable and self-cleaning. Exits non-zero if any assertion fails.
# Requires: go, curl. Chrome is optional (only for --with-chrome).

set -uo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

WITH_CHROME=0
[ "${1:-}" = "--with-chrome" ] && WITH_CHROME=1

WORK="$(mktemp -d)"
PROBE_DIR="$REPO/.dogfood_probe"
FIX=8091            # fixture server
WH_OK=9191          # webhook receiver (always 200)
WH_RETRY=9192       # webhook receiver (500 then 200)
API=18080           # daemon, SSRF guard OFF (functional tests)
API_ON=18081        # daemon, SSRF guard ON (rejection tests)
KEY="dogfood-secret"
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
hcode(){ curl -s -o /dev/null -w '%{http_code}' "$@"; }

cleanup(){
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null; done
  pkill -x loadstar 2>/dev/null; pkill -x dffixture 2>/dev/null; pkill -x dfwhook 2>/dev/null
  [ "$WITH_CHROME" = 1 ] && { pkill -x chrome 2>/dev/null; pkill -x headless_shell 2>/dev/null; }
  rm -rf "$WORK" "$PROBE_DIR"
}
trap cleanup EXIT

echo "== build =="
go build -o bin/loadstar ./cmd/loadstar || { echo "build loadstar failed"; exit 2; }

if [ "$WITH_CHROME" = 1 ]; then
  for c in /opt/pw-browsers/chromium-*/chrome-linux/chrome; do
    [ -x "$c" ] && ln -sf "$c" /usr/local/bin/google-chrome && break
  done
  command -v google-chrome >/dev/null || { echo "google-chrome not found; drop --with-chrome"; exit 2; }
  export LOADSTAR_CHROME_NO_SANDBOX=true   # needed when running as root in CI
fi

echo "== fixtures =="
cat > "$WORK/fixture.go" <<'GO'
package main

import ("fmt"; "net/http"; "os"; "strings"; "time")

func main() {
	addr := os.Args[1]
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>df</title></head><body><h1>df</h1><p>`+
			strings.Repeat("lorem ", 40)+`</p><img src="/img" width="200" height="100"></body></html>`)
	})
	mux.HandleFunc("/img", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		w.Write([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;"))
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second); w.Write([]byte("slow")) })
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	http.ListenAndServe(addr, mux)
}
GO
cat > "$WORK/whook.go" <<'GO'
package main

import ("fmt"; "io"; "net/http"; "os"; "sync/atomic")

func main() {
	addr, logf := os.Args[1], os.Args[2]
	var failFirst int64
	if len(os.Args) > 3 { fmt.Sscanf(os.Args[3], "%d", &failFirst) }
	var hits int64
	http.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		b, _ := io.ReadAll(r.Body)
		f, _ := os.OpenFile(logf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "HIT %d\n%s\n", n, string(b)); f.Close()
		if n <= failFirst { w.WriteHeader(500); return }
		w.WriteHeader(200)
	})
	http.ListenAndServe(addr, nil)
}
GO
( cd "$WORK" && go build -o dffixture fixture.go && go build -o dfwhook whook.go ) || { echo "fixture build failed"; exit 2; }
"$WORK/dffixture" 127.0.0.1:$FIX      >/dev/null 2>&1 & PIDS+=($!)
"$WORK/dfwhook" 127.0.0.1:$WH_OK    "$WORK/wh_ok.log"    0 >/dev/null 2>&1 & PIDS+=($!)
"$WORK/dfwhook" 127.0.0.1:$WH_RETRY "$WORK/wh_retry.log" 1 >/dev/null 2>&1 & PIDS+=($!)
sleep 1

# DB probe (needs the sqlite driver -> compiled inside the module)
mkdir -p "$PROBE_DIR"
cat > "$PROBE_DIR/main.go" <<'GO'
package main

import ("database/sql"; "fmt"; "os"; _ "github.com/mattn/go-sqlite3")

func main() {
	db, err := sql.Open("sqlite3", os.Args[1]+"?_foreign_keys=on")
	if err != nil { panic(err) }
	defer db.Close()
	var r, w int
	db.QueryRow("SELECT COUNT(*) FROM results WHERE job_id=?", os.Args[2]).Scan(&r)
	db.QueryRow("SELECT COUNT(*) FROM webhook_deliveries WHERE job_id=?", os.Args[2]).Scan(&w)
	fmt.Printf("results=%d webhooks=%d\n", r, w)
}
GO
probe(){ go run "$PROBE_DIR" "$1" "$2" 2>/dev/null; }

start_daemon(){ # $1=port $2=dbpath $3=key $4=guard(on|off)
  local envp="LOADSTAR_API_KEY=$3 LOADSTAR_LISTEN_ADDR=127.0.0.1:$1 DATABASE_URL=$2"
  # -u LOADSTAR_ALLOW_PRIVATE_IPS: this script exports it for the CLI tests, so a
  # guard-ON daemon must explicitly drop it (env adds to the environment).
  if [ "$4" = off ]; then
    env $envp LOADSTAR_ALLOW_PRIVATE_IPS=true bin/loadstar serve >"$WORK/d$1.log" 2>&1 & PIDS+=($!)
  else
    env -u LOADSTAR_ALLOW_PRIVATE_IPS $envp bin/loadstar serve >"$WORK/d$1.log" 2>&1 & PIDS+=($!)
  fi
  for i in $(seq 1 40); do curl -sf "http://127.0.0.1:$1/v1/health" >/dev/null 2>&1 && return 0; sleep 0.25; done
  echo "daemon on $1 did not become healthy"; return 1
}
poll(){ # $1=port $2=key $3=jobid -> prints terminal status
  local j st
  for i in $(seq 1 60); do
    j=$(curl -s -H "X-API-Key: $2" "http://127.0.0.1:$1/v1/jobs/$3")
    st=$(printf '%s' "$j" | grep -o '"status":"[A-Z]*"' | head -1 | cut -d'"' -f4)
    case "$st" in COMPLETED|FAILED|PARTIAL) echo "$st"; return;; esac
    sleep 0.25
  done; echo TIMEOUT
}

echo "== CLI (network) =="
export LOADSTAR_ALLOW_PRIVATE_IPS=true
out=$(bin/loadstar run -u http://127.0.0.1:$FIX/page -t network -f json 2>/dev/null)
assert_has "CLI network json has status_code 200" '"status_code": 200' "$out"
assert_has "CLI network json has response_bytes" '"response_bytes"' "$out"
blk=$(env -u LOADSTAR_ALLOW_PRIVATE_IPS bin/loadstar run -u http://127.0.0.1:$FIX/page -t network 2>&1)
assert_has "CLI SSRF guard blocks loopback" "private or restricted IP" "$blk"

if [ "$WITH_CHROME" = 1 ]; then
  echo "== CLI (browser/vitals) =="
  b=$(bin/loadstar run -u http://127.0.0.1:$FIX/page -t browser -f json 2>/dev/null)
  assert_has "CLI browser has page_load_ms" '"page_load_ms"' "$b"
  assert_has "CLI browser has waterfall" '"waterfall"' "$b"
  v=$(bin/loadstar run -u http://127.0.0.1:$FIX/page -t vitals -f json 2>/dev/null)
  assert_has "CLI vitals has fcp_ms" '"fcp_ms"' "$v"
fi

echo "== API daemon (guard OFF) =="
start_daemon $API "$WORK/api.db" "$KEY" off || exit 3
H="X-API-Key: $KEY"; B="http://127.0.0.1:$API"
assert_code "health public" 200 "$(hcode $B/v1/health)"
assert_code "auth: no key"   401 "$(hcode $B/v1/jobs)"
assert_code "auth: wrong"    401 "$(hcode -H 'X-API-Key: nope' $B/v1/jobs)"
assert_code "auth: correct"  200 "$(hcode -H "$H" $B/v1/jobs)"
assert_code "openapi.yaml"   200 "$(hcode $B/openapi.yaml)"
assert_code "runs=11 -> 400" 400 "$(hcode -H "$H" -X POST $B/v1/jobs -d '{"url":"http://127.0.0.1:'$FIX'/page","runs":11}')"
assert_code "missing url -> 400" 400 "$(hcode -H "$H" -X POST $B/v1/jobs -d '{}')"
assert_code "ftp scheme -> 400"  400 "$(hcode -H "$H" -X POST $B/v1/jobs -d '{"url":"ftp://x"}')"

jid=$(curl -s -H "$H" -X POST $B/v1/jobs -d '{"url":"http://127.0.0.1:'$FIX'/page","tiers":["network"]}' | grep -o 'jb_[0-9a-f-]*')
assert_code "create job accepted" 202 "$([ -n "$jid" ] && echo 202 || echo 000)"
assert_code "job completes" COMPLETED "$(poll $API "$KEY" "$jid")"

echo "== webhooks =="
wjid=$(curl -s -H "$H" -X POST $B/v1/jobs -d '{"url":"http://127.0.0.1:'$FIX'/page","tiers":["network"],"webhook_url":"http://127.0.0.1:'$WH_OK'/hook"}' | grep -o 'jb_[0-9a-f-]*')
poll $API "$KEY" "$wjid" >/dev/null; sleep 1
assert_has "webhook delivered with results payload" '"results"' "$(cat "$WORK/wh_ok.log" 2>/dev/null)"
rjid=$(curl -s -H "$H" -X POST $B/v1/jobs -d '{"url":"http://127.0.0.1:'$FIX'/page","tiers":["network"],"webhook_url":"http://127.0.0.1:'$WH_RETRY'/hook"}' | grep -o 'jb_[0-9a-f-]*')
poll $API "$KEY" "$rjid" >/dev/null; sleep 5
hits=$(grep -c '^HIT' "$WORK/wh_retry.log" 2>/dev/null || echo 0)
assert_code "webhook retried (>=2 hits)" ok "$([ "${hits:-0}" -ge 2 ] && echo ok || echo "hits=$hits")"

echo "== history + list =="
assert_has "history aggregates" '"test_count"' "$(curl -s -H "$H" "$B/v1/history?url=http://127.0.0.1:$FIX/page")"
assert_code "history missing param -> 400" 400 "$(hcode -H "$H" "$B/v1/history")"

echo "== DELETE -> FK cascade (audit fix) =="
djid=$(curl -s -H "$H" -X POST $B/v1/jobs -d '{"url":"http://127.0.0.1:'$FIX'/page","tiers":["network"],"webhook_url":"http://127.0.0.1:'$WH_OK'/hook"}' | grep -o 'jb_[0-9a-f-]*')
poll $API "$KEY" "$djid" >/dev/null; sleep 1
before=$(probe "$WORK/api.db" "$djid")
assert_has "before delete: rows exist" "results=1 webhooks=1" "$before"
assert_code "DELETE -> 204" 204 "$(hcode -H "$H" -X DELETE $B/v1/jobs/$djid)"
assert_code "GET deleted -> 404" 404 "$(hcode -H "$H" $B/v1/jobs/$djid)"
after=$(probe "$WORK/api.db" "$djid")
assert_has "after delete: cascade cleared rows" "results=0 webhooks=0" "$after"

echo "== API daemon (guard ON) — SSRF rejection =="
start_daemon $API_ON "$WORK/api_on.db" "k-on" on || exit 3
HO="X-API-Key: k-on"; BO="http://127.0.0.1:$API_ON"
assert_code "private-IP target rejected"   400 "$(hcode -H "$HO" -X POST $BO/v1/jobs -d '{"url":"http://127.0.0.1/"}')"
assert_code "metadata-IP target rejected"  400 "$(hcode -H "$HO" -X POST $BO/v1/jobs -d '{"url":"http://169.254.169.254/"}')"
assert_code "webhook SSRF rejected"        400 "$(hcode -H "$HO" -X POST $BO/v1/jobs -d '{"url":"https://example.com","webhook_url":"http://127.0.0.1/hook"}')"

echo
echo "==================================="
echo "  dogfood: PASS=$PASS FAIL=$FAIL"
echo "==================================="
[ "$FAIL" -eq 0 ]
