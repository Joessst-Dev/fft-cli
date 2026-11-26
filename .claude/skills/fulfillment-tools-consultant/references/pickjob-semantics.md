# Pickjob API semantics

Non-obvious behaviour of `/api/pickjobs*`. Locate the blocks with
`grep -n "^  /api/pickjobs" api/openapi/fft.api.swagger.yaml` and the schemas with
`grep -n "^    PickJob" …`.

1. **`PATCH /api/pickjobs/{pickJobId}` (`patchPickJob`) is deprecated** in favour of
   `POST /api/pickjobs/{pickJobId}/actions` (`pickJobAction`). The PATCH takes a *batch*
   (`actions[]`); the actions endpoint takes **exactly one action object per call** —
   `PickJobActionsParameter` is an `anyOf` of flat action schemas (abort, restart, reset, obsolete,
   replace assigned users, …), not an array. Do not model actions as a list.
2. **Every action carries `version`** next to `name`: optimistic locking lives in the body, and a
   stale version is a 409. There is no `If-Match` header. `PickJob` inherits a required `version`
   from `VersionedResource`.
3. **Two list endpoints, two pagination models, two projections:**
   - `GET /api/pickjobs` — `startAfterId` + `size` (default 25). It returns `StrippedPickJobs`: id,
     status, facility/order refs, carrier key, target time, search paths and version, **no line
     items**.
   - `POST /api/pickjobs/search` — cursor pagination (`after`/`before`/`size`/`last`, cursors from
     `pageInfo`), `size` up to 500. It returns **full `PickJob`s**. `total` is filled only when
     `options.withTotal: true`; `startAfterId` in the payload is deprecated in favour of `after`.
4. **`status` vs `subStatus`.** The string `statusReason` is deprecated; the typed `subStatus`
   replaces it. A short pick shows as status `PICKED`/`CLOSED` with subStatus `SHORT_PICKED` or
   `ZERO_PICKED`, so the status alone does not say a pick was incomplete.
5. `PickingMethodEnum` still lists `BATCH_PICKING`, deprecated in favour of `BATCH`.
6. `resetBlocked: true` on a pickjob means RESET/RESTART will be refused. Surface it before offering
   those actions.
