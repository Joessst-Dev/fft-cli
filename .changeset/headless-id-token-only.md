---
"fft": minor
---

`FFT_ID_TOKEN` now works on its own in headless mode: set `FFT_BASE_URL` and `FFT_ID_TOKEN` and fft runs from the environment, without the `FFT_FIREBASE_API_KEY` and `FFT_EMAIL` placeholders it used to demand. The password path is unchanged and still needs the base URL, the Firebase key and an email together.
