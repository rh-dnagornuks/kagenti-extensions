#!/usr/bin/env bash
#
# Test harness for init-iptables.sh "enforce-redirect" mode (proxy-sidecar
# fail-closed egress guard, capture variant).
#
# It validates, in a private network namespace:
#   1. Rule STRUCTURE — the AB_REDIRECT chain is hooked from nat OUTPUT at
#      position 1 with the expected RETURN exemptions, a `-p tcp` REDIRECT to
#      TRANSPARENT_PORT, and a terminal DROP; and no mangle rules are created.
#   2. CAPTURE (not drop) + AMBIENT ROBUSTNESS — external TCP egress is
#      REDIRECTed to TRANSPARENT_PORT, preempting a simulated Istio ambient
#      "nat OUTPUT REDIRECT" appended after our chain. Proven via packet
#      counters: our REDIRECT increments, the simulated ISTIO REDIRECT does not.
#   3. NON-TCP DROP — an external UDP datagram (QUIC/HTTP-3 bypass attempt) hits
#      the terminal DROP, proving non-TCP external egress cannot bypass.
#
# Requirements: root (for unshare --net + iptables), iproute2, iptables-nft,
# bash, the dummy kernel module. Runs on Linux / CI (e.g. ubuntu-latest); not on
# macOS. Uses `unshare --net` so it also works inside nested containers. Exit
# code 0 = all pass.
set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INIT="${INIT_SCRIPT:-${SCRIPT_DIR}/init-iptables.sh}"
IPT="${IPTABLES_CMD:-iptables-nft}"
EXTERNAL="198.51.100.7"   # RFC5737 TEST-NET-2, guaranteed unused
TPORT="8082"

# Re-exec into a private network namespace.
if [ -z "${_AB_NETNS_REEXEC:-}" ]; then
  exec unshare --net env _AB_NETNS_REEXEC=1 INIT_SCRIPT="${INIT}" \
       IPTABLES_CMD="${IPT}" bash "$0" "$@"
fi

fail=0

# Fresh netns: bring up lo and a dummy default route so packets to an external
# destination are actually generated and traverse the OUTPUT chain.
ip link set lo up
if ip link add eth-test type dummy 2>/dev/null; then
  ip addr add 10.255.255.2/24 dev eth-test
  ip link set eth-test up
  ip route add default via 10.255.255.1
else
  echo "WARN: dummy interface unavailable; capture packet may not be generated"
fi

echo "### Installing enforce-redirect rules"
env MODE=enforce-redirect PROXY_UID=1337 CLUSTER_CIDRS=10.0.0.0/8 \
    TRANSPARENT_PORT="${TPORT}" \
    IPTABLES_CMD="${IPT}" IP6TABLES_CMD=ip6tables-nft \
    sh "${INIT}" || { echo "FAIL: init script exited non-zero"; exit 1; }

dump=$("${IPT}" -t nat -S)
echo "--- nat ruleset ---"; echo "${dump}"

assert() { if echo "${dump}" | grep -qE "$2"; then echo "PASS: $1"; else echo "FAIL: $1"; fail=1; fi; }
assert "AB_REDIRECT hooked from OUTPUT" '^-A OUTPUT -j AB_REDIRECT'
assert "ztunnel mark RETURN"            'AB_REDIRECT .*mark.*0x539.*-j RETURN'
assert "proxy UID RETURN"               'AB_REDIRECT .*--uid-owner 1337 -j RETURN'
assert "loopback iface RETURN"          'AB_REDIRECT -o lo -j RETURN'
assert "loopback cidr RETURN"           'AB_REDIRECT -d 127.0.0.0/8 -j RETURN'
assert "cluster cidr RETURN"            'AB_REDIRECT -d 10.0.0.0/8 -j RETURN'
assert "tcp REDIRECT to transparent"    "AB_REDIRECT -p tcp -j REDIRECT --to-ports ${TPORT}"
assert "terminal DROP (non-tcp)"        'AB_REDIRECT -j DROP'

pos1=$("${IPT}" -t nat -L OUTPUT --line-numbers -n | awk '$1=="1"{print $2}')
if [ "${pos1}" = "AB_REDIRECT" ]; then echo "PASS: AB_REDIRECT at OUTPUT position 1"
else echo "FAIL: AB_REDIRECT not at OUTPUT position 1 (got '${pos1}')"; fail=1; fi

manglecount=$("${IPT}" -t mangle -S | grep -cE 'AB_REDIRECT|AB_EGRESS' || true)
if [ "${manglecount:-0}" -eq 0 ]; then echo "PASS: no mangle-table rules created"
else echo "FAIL: enforce-redirect created mangle rules"; fail=1; fi

echo "### Capture + preemption test: append a simulated ISTIO_OUTPUT nat REDIRECT"
"${IPT}" -t nat -A OUTPUT -p tcp -d "${EXTERNAL}" -j REDIRECT --to-ports 19999
# Generate an external TCP SYN (uid 0, like an agent bypass attempt). With no
# listener on TPORT the redirected SYN gets an RST; the rule counter still ticks.
timeout 2 bash -c "exec 3<>/dev/tcp/${EXTERNAL}/80" 2>/dev/null || true

capc=$("${IPT}" -t nat -L AB_REDIRECT -n -v | awk '/REDIRECT/{print $1; exit}')
istioc=$("${IPT}" -t nat -L OUTPUT -n -v | awk '/REDIRECT/{print $1; exit}')
echo "AB_REDIRECT REDIRECT pkts=${capc:-?} | simulated ISTIO REDIRECT pkts=${istioc:-?}"
if [ "${capc:-0}" -gt 0 ] && [ "${istioc:-0}" -eq 0 ]; then
  echo "PASS: external TCP captured to transparent port, preempting nat REDIRECT (ambient-robust)"
else
  echo "FAIL: capture/preemption not demonstrated (AB=${capc:-?}, ISTIO=${istioc:-?})"; fail=1
fi

echo "### Non-TCP drop test: send an external UDP datagram (QUIC bypass attempt)"
timeout 2 bash -c "echo -n x >/dev/udp/${EXTERNAL}/53" 2>/dev/null || true
dropc=$("${IPT}" -t nat -L AB_REDIRECT -n -v | awk '/DROP/{print $1; exit}')
echo "AB_REDIRECT DROP pkts=${dropc:-?}"
if [ "${dropc:-0}" -gt 0 ]; then
  echo "PASS: external UDP dropped (HTTP/3 cannot bypass)"
else
  echo "FAIL: external UDP not dropped (DROP=${dropc:-?})"; fail=1
fi

echo
[ "${fail}" -eq 0 ] && echo "ALL TESTS PASSED" || echo "SOME TESTS FAILED"
exit "${fail}"
