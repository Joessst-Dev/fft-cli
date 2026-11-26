---
"fft": patch
---

`fft routing decision-logs --all` no longer fails with a confusing `HTTP 404: Invalid ObjectId string` on the second page. The API cannot page this list past its first page, so `--all` now reads one page of up to 250 logs and warns on stderr when there are more, pointing you at the filters (`--order`, `--tenant-order-id`, `--routing-plan`) to narrow the search.
