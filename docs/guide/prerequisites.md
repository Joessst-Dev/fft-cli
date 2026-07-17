---
title: Before you begin
---

# Before you begin

You need four things from your fulfillmenttools onboarding email or your admin. They are
not all obvious, so:

| What | Looks like | Notes |
|---|---|---|
| **Base URL** | `https://ocff-acme-pre.api.fulfillmenttools.com` | The **full host**. Don't try to build it from the project id — the official docs disagree with themselves about the prefix. Copy whatever a working curl call or the onboarding email uses. |
| **Firebase Web API key** | `AIzaSy…` | The value in `?key=` on the Google sign-in URL. **This is not a credential** — see [Authentication](./auth.md). |
| **Username** | `jane.doe` | Your login name, *not* an email address. |
| **Password** | | |

Plus the **project id** (`acme`) and **environment** (`pre`, `prod`, …) — fft needs them
to derive your sign-in email, which is synthetic:
`{username}@ocff-{projectId}-{env}.com`. If you already know the full email, pass that
instead and the other two become unnecessary.
