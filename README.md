# Attend-Sync

HTTP API that connects to an attendance device (ZKTeco TCP, port 4370) and
returns its attendance log as JSON.

```bash
go run ./cmd/server          # or: docker compose up --build
```

## Endpoints

| Method | Path                     | Purpose                                            |
| ------ | ------------------------ | -------------------------------------------------- |
| POST   | `/api/attendance`        | Pull attendance records (optionally `from`/`to`)    |
| POST   | `/api/device/test`       | Connect + handshake check                          |
| POST   | `/api/device/info`       | Serial, device name, firmware                      |
| POST   | `/api/device/diagnose`   | TCP probe + handshake, with hints on failure       |
| POST   | `/api/device/clock`      | Read the device RTC and compare it with the server |
| POST   | `/api/device/clock/sync` | Set the device RTC to the server time              |
| GET    | `/api/health`            | Liveness                                           |

Every request body carries the device inline:

```json
{
  "device": {
    "id": "01",
    "name": "Main",
    "type": "zkteco",
    "host": "192.168.1.201",
    "port": 4370,
    "password": "0"
  },
  "from": "2026-09-01T00:00:00+05:45",
  "to": "2026-09-30T23:59:59+05:45"
}
```

`password` is the device COMM key (integer, `0` when unset); it is sent via
`CMD_AUTH` only when the device answers `2005` to the connect.

## Why timestamps can read 2000-01-01

ZKTeco devices do not store a Unix timestamp. They store their own packed
calendar value, and the earliest value they can represent is
**2000-01-01T00:00:00** — the scalar `0`. A record stamped with `0` means the
device wrote it while its RTC was unset (flat/dead clock battery, or never
configured), so the real instant was never recorded anywhere and cannot be
recovered from the device.

`POST /api/attendance` therefore returns the stored values unchanged and adds:

```json
"clock": {
  "device_time": "2000-01-01T00:00:00Z",
  "server_time": "2026-09-16T18:30:00+05:45",
  "drift_seconds": -843000000,
  "in_sync": false,
  "warning": "device clock reads ... but the server reads ..."
},
"warning": "all 1472 records are stamped before 2010 ..."
```

Timestamps are never rewritten to "today": a fabricated punch time is worse
than an obviously wrong one. Fix the source instead:

```bash
curl -X POST localhost:8080/api/device/clock/sync \
  -H 'Content-Type: application/json' \
  -d '{"device":{"id":"01","type":"zkteco","host":"192.168.1.201","port":4370}}'
```

The response's `applied` field confirms the write took effect by reading the
clock back. Only **future** punches get the correct date; records already
stored keep their original timestamps.

## Record decoding

The bulk payload is `total_size (u32) + records`, where each record is 8, 16
or 40 bytes. The grid is derived the way the reference implementation does:
`record_size = total_size / records`, with `records` read from the device via
`CMD_GET_FREE_SIZES`. This matters — a wrong grid shifts every field, which
produces plausible-looking but wrong user ids and dates. The server log states
the decision and flags a disagreement:

```
[zkteco] att blob: len=23556 framed=true total_size=23552 device_records=1472 grid=16 parsed=1472 head=...
[zkteco] WARNING parsed 3 records but the device reports 1472 (grid=40) — timestamps/user ids from this payload are suspect
```

## Troubleshooting ladder

1. `POST /api/device/diagnose` — separates TCP reachability from handshake.
2. `dial tcp ... i/o timeout` and `ping` fails → the host is not on the
   device's subnet (check `ipconfig getifaddr en0` against the device IP).
3. `connection reset by peer` right after TCP open → the device allows a
   single session: close ZKBio/other tools and other integrations, power-cycle
   the device, then retry **once**.
4. `unauthenticated (2005)` → the COMM key in `password` is wrong.
5. Records look wrong → check the `clock` block and the `[zkteco] att blob`
   log line above.

## Tests

```bash
go test ./...
```

`internal/device/zkteco` and `internal/attendance` test against byte-for-byte
golden vectors generated from the reference ZK implementation (`pyzk`):
checksums, packet framing, comm-key scrambling, the 8/16/40-byte record
layouts, time encoding/decoding, the record-grid derivation and the
unset-clock warnings — so a mis-aligned field fails the build instead of
silently returning year-2000 timestamps.
