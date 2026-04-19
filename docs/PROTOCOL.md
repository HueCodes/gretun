# Protocols

Two wire surfaces: the **disco envelope** (UDP + relay-queued) and the
**coordinator HTTP API** (node ↔ coord). Both are versioned as v1.

## 1. Disco envelope

Deliberately mirrors Tailscale's `disco` wire format so the trust story
is recognisable. Carried either on the shared UDP socket between peers,
or tunnelled byte-for-byte through the coordinator relay.

```
Offset  Size  Field
0       6     Magic   "TS" 0xF0 0x9F 0x92 0xAC    (the 💬 emoji)
6       32    Sender  Curve25519 pubkey of the sender (disco key, public half)
38      24    Nonce   NaCl-box nonce
62      N     Body    nacl/box ciphertext of UTF-8 JSON (see below)
```

The body is sealed with `nacl/box(plaintext, nonce, recipient.Pub, sender.Priv)`.
`magic + sender` travels in the clear: the coordinator uses `sender` as
the address for relay queueing; the recipient's `sender` knowledge is
what lets it find the right static shared key to unseal.

### Body messages (JSON, v1)

```json
// type=ping:  cold-path probe to validate a candidate path
{ "type": "ping",          "tx": "<16B hex>", "node_key": "<b64 Ed25519 pubkey>" }

// type=pong:  response to a ping, containing the src we saw it from
{ "type": "pong",          "tx": "<16B hex>", "src": "1.2.3.4:5555" }

// type=call_me_maybe:  "here are my endpoints, try them"
{ "type": "call_me_maybe", "endpoints": ["1.2.3.4:5555", "10.0.0.2:5555"] }
```

`tx` is a 16-byte hex transaction ID used to correlate a pong with its
ping. `endpoints` is a list of `ip:port` strings — each is both a local
interface address and the STUN-reported public mapping.

### Transport

- **Direct UDP**: the daemon writes `magic + sender + sealed` as one UDP
  datagram to the peer's candidate endpoint. The peer reads, parses,
  unseals, and dispatches.
- **Relayed**: the daemon POSTs the same bytes to `POST /v1/signal` as
  `{"to": <peer.DiscoKey>, "sealed": <bytes>}`. The coordinator queues
  them against `to`; the recipient pulls with `GET /v1/signal`.

In either case, the **on-the-wire envelope bytes are identical** — which
is why the magic/sender/sealed framing is structured this way.

## 2. Coordinator HTTP API

All request bodies are JSON; responses too. HTTPS in production, plain
HTTP for development. Timestamps are RFC 3339 (UTC) or UNIX seconds.

### Authentication

Every endpoint except `POST /v1/register` requires three headers:

```
X-Gretun-Timestamp: <unix seconds>
X-Gretun-Node:      <base64 Ed25519 pubkey>
Authorization:      Gretun <base64 Ed25519 signature>
```

The signature covers a fixed digest:

```
digest = SHA256(body)
signed = timestamp || "\n" || method || "\n" || path || "\n" || digest
sig    = ed25519.Sign(nodePriv, signed)
```

Requests with `|now - timestamp| > 60s` are rejected as stale. A valid
signature proves possession of `nodePriv` AND freshness.

### Endpoints

```
POST /v1/register
  req:  { node_pubkey: <b64>, disco_pubkey: <b64>, node_name, requested_tunnel_ip? }
  resp: { tunnel_ip: "100.64.0.5/24", peers_etag: "..." }

POST /v1/endpoints
  req:  { endpoints: [{addr: "1.2.3.4:5555", source: "local"|"stun"}, ...] }
  resp: { ok: true }

GET  /v1/peers?since=<etag>
  - Long-poll: server holds the connection up to 25s waiting for `etag != since`.
  resp: { etag, peers: [{ node_pubkey, disco_pubkey, node_name, tunnel_ip, endpoints, updated_at }] }

POST /v1/signal
  req:  { to: <b64 disco pubkey>, sealed: <b64 envelope bytes> }
  resp: { queued: true }

GET  /v1/signal
  - Long-poll: server holds up to 25s waiting for an envelope addressed to
    the authenticated caller's disco key. Looks up disco key by node key.
  resp: { envelopes: [{ sealed: <b64>, enqueue }] }

GET  /debug/peers
  - Unauthenticated; useful for inspection during development.
  resp: same shape as /v1/peers.
```

### Tunnel IP assignment

The coordinator draws from a configurable CIDR (`--pool`, default
`100.64.0.0/24`). Allocation is stable: `nodePubkey → tunnelIP` is
persisted so a reconnecting node keeps its address.

### Relay semantics

- Per-recipient queue capped at 64 envelopes; oldest drop when full.
- Envelopes older than 30 seconds are dropped on pull.
- The coordinator never decrypts or inspects `sealed` — only routes by `to`.
