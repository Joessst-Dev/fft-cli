---
title: Read-only projects
---

# Read-only projects

A project can be marked read-only. `fft` then refuses every request that would change
the tenant — creates, updates, patches, deletes, action endpoints — **before it signs
in**, so a refused write costs no round trip and mints no token. It exits `10`.

```sh
fft project add prod --base-url … --read-only   # protected from the start
fft project read-only prod                      # or protect one you already have
fft project read-only prod --off                # asks before it re-arms writes
```

Three ways to say it, and they only ever tighten:

| | |
|---|---|
| `readOnly: true` in the config | the durable one, per project; shown in `fft project list` |
| `FFT_READ_ONLY=1` | protects whichever project `fft` is about to use — the CI knob |
| `--read-only` on any command | protects that one invocation |

`--read-only=false` cannot loosen a project that is configured read-only, or a set
`FFT_READ_ONLY`. It is refused as a usage error rather than quietly honoured: a
guardrail that a flag can switch off is one a copied-and-pasted command line switches
off.

The one place `--read-only=false` *does* mean something is `fft project add --force`,
where it is how you take the mark off a project you are reconfiguring — and there, like
`fft project read-only --off`, it asks before it re-arms writes. Re-adding a protected
project **without** saying `--read-only=false` leaves the mark on; rotating a password
does not silently disarm prod.

## Reads keep working, and that includes the searches

The fulfillmenttools API runs its cursor searches over `POST` — `POST
/api/facilities/search` is a read — so being read-only is *not* the same as refusing to
`POST`. Which POST is which cannot be guessed from the path (only 34 of the 46 read-POSTs
end in `/search`) nor from the status code (43 mutating POSTs answer `200`, not `201`),
so `fft` carries an explicit, hand-curated allowlist of the POSTs that read, and treats
every other one as a write. `postDeliveryPromise` sits in the same family as the pure
delivery calculators and reserves stock, so it is blocked; `evaluateRoutingStrategy` is a
dry run, so it is not. A POST the API grows tomorrow is a write until a human says
otherwise, and the build fails until one does.

See also [Discovery](./discovery.md) for how to tell a read from a write before you run
it, and [Troubleshooting](./troubleshooting.md) for the exit codes.
