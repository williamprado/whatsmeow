# interactive-test

Manual smoke test for the [interactive message helpers](../../docs/interactive_messages.md).
It logs in via QR code (printed as ASCII in the terminal) and sends a batch of
button messages to a recipient.

> ⚠️ **EXPERIMENTAL — account-ban risk.** Sending interactive messages from
> non-official libraries is experimental and WhatsApp may **ban** the sending
> account. Scan the QR code **only with a disposable test account**. **Never use
> the production (atendzappy) account.**

## Run

```sh
# recipient phone number, digits only, no "+" (example value below)
export TEST_RECIPIENT=5577988272902
go run ./examples/interactive-test
```

On Windows PowerShell:

```powershell
$env:TEST_RECIPIENT = "5577988272902"
go run ./examples/interactive-test
```

1. Scan the QR code shown in the terminal with your **test** WhatsApp account
   (this is the *sending* account).
2. The program sends test messages to `TEST_RECIPIENT` and logs each result.
3. Press `Ctrl+C` to disconnect and exit.

The session is cached in `session.db` (gitignored), so the QR is only needed on
the first run. Delete that file to force a fresh login.

## What renders

With the current send core, only **`BuildButtonsMessage`** and
**`BuildListMessage`** render on the recipient's device. The template,
native-flow and carousel messages are sent too (to exercise the helpers), but
they will **not** render until the `send.go` core patch described in
[docs/interactive_messages.md](../../docs/interactive_messages.md) is applied and
validated. The program annotates those sends in its output.

## Notes

- Uses the pure-Go `modernc.org/sqlite` driver, so no C compiler is needed.
- The recipient number is read from `TEST_RECIPIENT` and is **not** hardcoded.
- This example is for manual runs only; it is not part of CI.
